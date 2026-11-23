package main

import "net/http"

// Installed clients may still send the old identify or start request.
func registrationRequired(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusForbidden, "registration_required", "请先注册或登录。新账号自注册成功起可免费试用 15 分钟。")
}
