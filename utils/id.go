package utils

import (
	gonanoid "github.com/matoous/go-nanoid/v2"
)

var defaultAlphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func NewID() string {
	// 62 ** 22 > 64 ** 21
	// 初始为22位，现在是16位
	// 按照计算器，https://zelark.github.io/nano-id-cc/，每秒生成1000个，约981年不会碰撞
	id, err := gonanoid.Generate(defaultAlphabet, 16)
	if err != nil {
		panic(err)
	}
	return id
}
