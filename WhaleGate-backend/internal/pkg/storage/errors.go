package storage

import "errors"

// 存储配置错误：均由管理员配置缺失或非法引起，需明确提示而非静默降级。
var (
	// ErrEmptyRoot 本地存储未配置根目录。
	ErrEmptyRoot = errors.New("本地存储未配置根目录")
	// ErrInvalidKey 对象键非法。
	ErrInvalidKey = errors.New("对象键非法")
	// ErrNoBucket S3 存储未配置桶名。
	ErrNoBucket = errors.New("对象存储未配置桶名")
	// ErrNoEndpoint S3 存储未配置服务地址。
	ErrNoEndpoint = errors.New("对象存储未配置服务地址")
	// ErrNoCredentials S3 存储未配置访问密钥。
	ErrNoCredentials = errors.New("对象存储未配置访问密钥")
)
