package handler

import (
	"encoding/json"
	"errors"

	"github.com/whalegate/whalegate/internal/pkg/apierr"
)

// errInvalidID 路径参数非法。
var errInvalidID = apierr.New(apierr.ErrInvalidParam, "ID 参数非法")

// bindError 把参数绑定/校验错误转为业务错误。
// 明细仅写入上下文用于日志排查，不会返回给客户端。
func bindError(err error) error {
	if err == nil {
		return nil
	}
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &syntaxErr) || errors.As(err, &typeErr) {
		return apierr.Wrap(apierr.ErrInvalidJSON, err)
	}
	return apierr.Wrap(apierr.ErrInvalidParam, err)
}
