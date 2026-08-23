package upstream

import (
	"context"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

func (material AuthMaterial) withPasswordLogin(input LoginInput) AuthMaterial {
	username := strings.TrimSpace(input.Username)
	if input.Mode != model.UpstreamAuthPassword || username == "" || input.Password == "" {
		return material
	}
	material.LoginUsername = username
	material.LoginPassword = input.Password
	return material
}

func (material AuthMaterial) passwordLoginInput() (LoginInput, bool) {
	username := strings.TrimSpace(material.LoginUsername)
	if username == "" || material.LoginPassword == "" {
		return LoginInput{}, false
	}
	return LoginInput{
		Mode:     model.UpstreamAuthPassword,
		Username: username,
		Password: material.LoginPassword,
	}, true
}

func reloginSub2API(ctx context.Context, client *remoteClient, material AuthMaterial, fallback error) (sub2APIUser, AuthMaterial, error) {
	input, ok := material.passwordLoginInput()
	if !ok || AsAdapterError(fallback).Status == model.IdentityStatusAccessDenied {
		return sub2APIUser{}, material, fallback
	}
	relogged, _, err := sub2APILoginMaterial(ctx, client, input)
	if err != nil {
		return sub2APIUser{}, material, err
	}
	response, err := client.do(ctx, http.MethodGet, "/api/v1/auth/me", nil, relogged)
	if err != nil {
		return sub2APIUser{}, material, adapterError("UPSTREAM_NETWORK_ERROR", "无法验证 Sub2API 登录状态", model.IdentityStatusNetworkError, http.StatusBadGateway)
	}
	var user sub2APIUser
	if err := decodeSub2APIResponse(response, &user); err != nil {
		return sub2APIUser{}, material, err
	}
	return user, relogged, nil
}

func reloginNewAPI(ctx context.Context, client *remoteClient, material AuthMaterial, fallback error) (newAPIUser, AuthMaterial, error) {
	input, ok := material.passwordLoginInput()
	if !ok || AsAdapterError(fallback).Status == model.IdentityStatusAccessDenied {
		return newAPIUser{}, material, fallback
	}
	relogged, _, err := newAPILoginMaterial(ctx, client, input)
	if err != nil {
		return newAPIUser{}, material, err
	}
	response, err := client.do(ctx, http.MethodGet, "/api/user/self", nil, relogged)
	if err != nil {
		return newAPIUser{}, material, adapterError("UPSTREAM_NETWORK_ERROR", "无法验证 NewAPI 登录状态", model.IdentityStatusNetworkError, http.StatusBadGateway)
	}
	var user newAPIUser
	if err := decodeNewAPIResponse(response, &user); err != nil {
		return newAPIUser{}, material, err
	}
	if relogged.UserID == "" && user.ID > 0 {
		relogged.UserID = positiveNewAPIUserID(user.ID)
	}
	return user, relogged, nil
}
