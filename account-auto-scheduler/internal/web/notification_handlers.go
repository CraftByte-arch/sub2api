package web

import (
	"errors"
	"net/http"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/engine"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/notify"
	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/store"
)

func (s *Server) handleNotificationSettings(w http.ResponseWriter, _ *http.Request) {
	if !s.requireNotificationConsole(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": s.notifications.Settings()})
}

func (s *Server) handleSaveNotificationSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireNotificationConsole(w) {
		return
	}
	var request notificationSettingsRequest
	if err := decodeLimitedJSON(r, 64<<10, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_NOTIFICATION_SETTINGS", err.Error())
		return
	}
	settings, err := s.notifications.SaveSettings(notify.SettingsInput{
		Enabled:           request.Enabled,
		BarkEndpoint:      request.BarkEndpoint,
		BarkBasicAuthUser: request.BarkBasicAuthUser,
		DeviceKey:         request.DeviceKey,
		EncryptionKey:     request.EncryptionKey,
		BasicAuthPassword: request.BasicAuthPassword,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_NOTIFICATION_SETTINGS", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings})
}

func (s *Server) handleClearNotificationSettings(w http.ResponseWriter, _ *http.Request) {
	if !s.requireNotificationConsole(w) {
		return
	}
	if err := s.notifications.ClearSettings(); err != nil {
		writeError(w, http.StatusBadGateway, "NOTIFICATION_CLEAR_FAILED", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTestNotification(w http.ResponseWriter, r *http.Request) {
	if !s.requireNotificationConsole(w) {
		return
	}
	if err := s.notifications.Test(r.Context()); err != nil {
		writeError(w, http.StatusBadGateway, "NOTIFICATION_TEST_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}

func (s *Server) handleSetGroupBalanceAlert(w http.ResponseWriter, r *http.Request) {
	if !s.requireNotificationConsole(w) {
		return
	}
	groupID, err := pathPositiveID(r, "groupID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_GROUP", err.Error())
		return
	}
	if !s.groupExists(r, groupID, w) {
		return
	}
	request, err := decodeBalanceAlertRequest(r)
	if err != nil || request.Threshold == nil {
		writeError(w, http.StatusBadRequest, "INVALID_BALANCE_ALERT", "余额告警阈值不能为空")
		return
	}
	if err := s.notifications.SetGroupBalanceThreshold(groupID, request.Threshold); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BALANCE_ALERT", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"group_id": groupID, "threshold": *request.Threshold})
}

func (s *Server) handleClearGroupBalanceAlert(w http.ResponseWriter, r *http.Request) {
	if !s.requireNotificationConsole(w) {
		return
	}
	groupID, err := pathPositiveID(r, "groupID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_GROUP", err.Error())
		return
	}
	if err := s.notifications.SetGroupBalanceThreshold(groupID, nil); err != nil {
		writeError(w, http.StatusBadRequest, "BALANCE_ALERT_CLEAR_FAILED", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSetAccountBalanceAlert(w http.ResponseWriter, r *http.Request) {
	if !s.requireNotificationConsole(w) {
		return
	}
	accountID, err := pathAccountID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ACCOUNT", err.Error())
		return
	}
	request, err := decodeBalanceAlertRequest(r)
	if err != nil || request.Threshold == nil {
		writeError(w, http.StatusBadRequest, "INVALID_BALANCE_ALERT", "余额告警阈值不能为空")
		return
	}
	if s.engine == nil {
		writeError(w, http.StatusServiceUnavailable, "CONSOLE_UNAVAILABLE", "账号设置暂不可用")
		return
	}
	if _, err := s.engine.EnsurePassiveAccount(r.Context(), accountID); err != nil {
		status := http.StatusBadGateway
		code := "ACCOUNT_SETTINGS_FAILED"
		if errors.Is(err, engine.ErrUnsupportedAccount) {
			status = http.StatusBadRequest
			code = "UNSUPPORTED_ACCOUNT"
		}
		writeError(w, status, code, err.Error())
		return
	}
	if err := s.notifications.SetAccountBalanceThreshold(accountID, request.Threshold); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "账号设置不存在")
			return
		}
		writeError(w, http.StatusBadRequest, "INVALID_BALANCE_ALERT", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account_id": accountID, "threshold": *request.Threshold})
}

func (s *Server) handleClearAccountBalanceAlert(w http.ResponseWriter, r *http.Request) {
	if !s.requireNotificationConsole(w) {
		return
	}
	accountID, err := pathAccountID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ACCOUNT", err.Error())
		return
	}
	if err := s.notifications.SetAccountBalanceThreshold(accountID, nil); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeError(w, http.StatusBadRequest, "BALANCE_ALERT_CLEAR_FAILED", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) requireNotificationConsole(w http.ResponseWriter) bool {
	if s.notifications != nil {
		return true
	}
	writeError(w, http.StatusServiceUnavailable, "NOTIFICATIONS_UNAVAILABLE", "通知服务暂不可用")
	return false
}

func (s *Server) groupExists(r *http.Request, groupID int64, w http.ResponseWriter) bool {
	if s.console == nil {
		return true
	}
	groups, err := s.console.ListGroups(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "GROUPS_UNAVAILABLE", err.Error())
		return false
	}
	if _, found := findGroup(groups, groupID); !found {
		writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "分组不存在")
		return false
	}
	return true
}

func decodeBalanceAlertRequest(r *http.Request) (balanceAlertRequest, error) {
	var request balanceAlertRequest
	if err := decodeLimitedJSON(r, 8<<10, &request); err != nil {
		return balanceAlertRequest{}, err
	}
	return request, nil
}
