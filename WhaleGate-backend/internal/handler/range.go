package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/whalegate/whalegate/internal/pkg/storage"
)

// videoRange 解析后的字节区间；ok 为 false 表示请求未带有效的 Range 头。
type videoRange struct {
	start int64
	end   int64
	ok    bool
}

// parseRange 解析 `Range: bytes=start-end`。
//
// 只支持单区间：视频播放器的 seek 请求都是单区间，
// 多区间（逗号分隔）在浏览器场景不会出现，解析失败就退回整段 200。
func parseRange(header string, size int64) videoRange {
	if size <= 0 || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(header)), "bytes=") {
		return videoRange{}
	}
	spec := strings.TrimSpace(header[len("bytes="):])
	// 多区间直接放弃分段，按整段返回即可正常工作。
	if strings.Contains(spec, ",") {
		return videoRange{}
	}
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return videoRange{}
	}

	var start, end int64
	switch {
	case parts[0] == "" && parts[1] != "":
		// bytes=-N：末尾 N 字节
		n, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || n <= 0 {
			return videoRange{}
		}
		start = size - n
		if start < 0 {
			start = 0
		}
		end = size - 1
	case parts[0] != "" && parts[1] == "":
		// bytes=N-：从 N 到末尾
		n, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || n < 0 || n >= size {
			return videoRange{}
		}
		start = n
		end = size - 1
	default:
		s, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || s < 0 {
			return videoRange{}
		}
		e, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || e < s {
			return videoRange{}
		}
		start = s
		end = e
		if end >= size {
			end = size - 1
		}
	}
	if start >= size {
		return videoRange{}
	}
	return videoRange{start: start, end: end, ok: true}
}

// writeVideo 把一段视频写入响应，支持 HTTP Range（206 分段）。
//
// 视频播放器拖动进度条会发 Range 请求：不支持时浏览器要么无法 seek，
// 要么每次都重新拉取整段。取不到大小的后端（或没带 Range）仍按整段 200 返回。
func writeVideo(
	c *gin.Context,
	store storage.Storage,
	key string,
	contentType string,
	fileName string,
) error {
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", `inline; filename="`+fileName+`"`)
	// 视频不可变，允许浏览器私有缓存，避免作品库翻页时重复拉取。
	c.Header("Cache-Control", "private, max-age=86400")

	size, err := store.Size(c.Request.Context(), key)
	if err != nil || size <= 0 {
		// 拿不到长度就没法应答 Range，按整段输出即可。
		return writeFullVideo(c, store, key)
	}
	c.Header("Accept-Ranges", "bytes")

	requested := parseRange(c.GetHeader("Range"), size)
	if !requested.ok {
		return writeFullVideo(c, store, key, size)
	}

	reader, err := store.OpenRange(c.Request.Context(), key, requested.start, requested.end)
	if err != nil {
		// 分段失败不应让播放器彻底拿不到内容，退回整段。
		return writeFullVideo(c, store, key, size)
	}
	defer reader.Close()

	length := requested.end - requested.start + 1
	c.Header("Content-Range", "bytes "+strconv.FormatInt(requested.start, 10)+"-"+
		strconv.FormatInt(requested.end, 10)+"/"+strconv.FormatInt(size, 10))
	c.Header("Content-Length", strconv.FormatInt(length, 10))
	c.Status(http.StatusPartialContent)
	written, err := io.Copy(c.Writer, reader)
	if err != nil && written < length {
		// 客户端中途断开属于正常现象，不视为错误。
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	return nil
}

// writeFullVideo 输出整段视频（200），size 为 0 时不定长。
func writeFullVideo(c *gin.Context, store storage.Storage, key string, sizes ...int64) error {
	reader, err := store.Open(c.Request.Context(), key)
	if err != nil {
		return err
	}
	defer reader.Close()
	if len(sizes) > 0 && sizes[0] > 0 {
		c.Header("Content-Length", strconv.FormatInt(sizes[0], 10))
	}
	c.Status(http.StatusOK)
	_, err = io.Copy(c.Writer, reader)
	return err
}
