package main

import (
	"crypto/subtle"
	"encoding/base64"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 7 || parts[0] != "argon2id" || parts[1] != "19" {
		return false
	}
	memory, err1 := strconv.ParseUint(parts[2], 10, 32)
	iterations, err2 := strconv.ParseUint(parts[3], 10, 32)
	parallelism, err3 := strconv.ParseUint(parts[4], 10, 8)
	salt, err4 := base64.RawStdEncoding.DecodeString(parts[5])
	want, err5 := base64.RawStdEncoding.DecodeString(parts[6])
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil || len(salt) < 16 || len(want) != 32 || memory < 32*1024 || memory > 256*1024 || iterations < 2 || iterations > 8 || parallelism < 1 || parallelism > 8 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, uint32(iterations), uint32(memory), uint8(parallelism), uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}
