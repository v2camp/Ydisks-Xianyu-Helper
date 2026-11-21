package server

import (
	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
)

// mountVersionedSettingsCardNotificationRoutes 挂载设置、卡券和通知的 `/api/v1` 兼容入口。
func (s *Server) mountVersionedSettingsCardNotificationRoutes(r chi.Router) {
	// 公开系统设置只读取允许匿名访问的主题等配置。
	r.Get("/api/v1/settings/system/public", s.publicSettings)

	// 管理员设置入口保持旧路径的管理员权限边界。
	r.Group(func(r chi.Router) {
		r.Use(s.Auth.Middleware)
		r.Use(auth.RequireAuth)
		r.Use(auth.RequireAdmin)
		r.Get("/api/v1/settings/system", s.allSettings)
		r.Put("/api/v1/settings/system", s.setSettings)
		r.Put("/api/v1/settings/system/{key}", s.setSetting)
		r.Get("/api/v1/admin/notifications/outbox/uncertain", s.listAdminUncertainNotifications)
		r.Get("/api/v1/admin/notifications/system-events", s.listAdminSystemEvents)
		r.Post("/api/v1/admin/notifications/system-events/{event_id}/read", s.markAdminSystemEventRead)
		r.Post("/api/v1/admin/notifications/system-events/read-all", s.markAllAdminSystemEventsRead)

		// 系统告警渠道与租户渠道完全隔离，仅管理员可维护。
		r.Get("/api/v1/admin/system-alert-channels", s.listSystemAlertChannels)
		r.Get("/api/v1/admin/system-alert-channels/{channel_id}", s.getSystemAlertChannelEditor)
		r.Post("/api/v1/admin/system-alert-channels", s.createSystemAlertChannel)
		r.Put("/api/v1/admin/system-alert-channels/{channel_id}", s.updateSystemAlertChannel)
		r.Delete("/api/v1/admin/system-alert-channels/{channel_id}", s.deleteSystemAlertChannel)
		r.Post("/api/v1/admin/system-alert-channels/{channel_id}/test", s.testSystemAlertChannel)
	})

	// 普通登录用户可访问账号 AI 设置、用户设置、卡券和通知资源。
	r.Group(func(r chi.Router) {
		r.Use(s.Auth.Middleware)
		r.Use(auth.RequireAuth)

		r.Get("/api/v1/settings/ai-reply", s.listAIReply)
		r.Get("/api/v1/settings/ai-reply/{cookie_id}", s.getAIReply)
		r.Put("/api/v1/settings/ai-reply/{cookie_id}", s.setAIReply)
		r.Get("/api/v1/settings/user", s.listUserSettings)
		r.Get("/api/v1/settings/user/integrations", s.getUserIntegrations)
		r.Put("/api/v1/settings/user/integrations", s.setUserIntegrations)
		r.Post("/api/v1/settings/user/ai-models", s.listAIModels)
		r.Post("/api/v1/settings/user/ai-test", s.testAIConnection)
		r.Put("/api/v1/settings/user/{key}", s.setUserSetting)
		r.Get("/api/v1/settings/user/{key}", s.getUserSetting)

		r.Get("/api/v1/cards", s.listCards)
		r.Post("/api/v1/cards", s.createCard)
		r.Post("/api/v1/cards/test-api", s.testCardAPI)
		r.Post("/api/v1/cards/batch", s.batchCreateCards)
		r.Post("/api/v1/cards/{card_id}/append-data", s.appendCardData)
		r.Get("/api/v1/cards/{card_id}/details", s.getCard)
		r.Get("/api/v1/cards/{card_id}", s.getCard)
		r.Put("/api/v1/cards/{card_id}", s.updateCard)
		r.Delete("/api/v1/cards/{card_id}", s.deleteCard)

		r.Get("/api/v1/notifications/channels", s.listChannels)
		r.Get("/api/v1/notifications/channels/{channel_id}", s.getChannelEditor)
		r.Post("/api/v1/notifications/channels", s.createChannel)
		r.Put("/api/v1/notifications/channels/{channel_id}", s.updateChannel)
		r.Delete("/api/v1/notifications/channels/{channel_id}", s.deleteChannel)
		r.Post("/api/v1/notifications/channels/{channel_id}/test", s.testChannel)
		r.Get("/api/v1/notifications/outbox/uncertain", s.listUncertainNotifications)
		r.Get("/api/v1/notifications/events", s.listNotificationEvents)
		r.Post("/api/v1/notifications/events/{event_id}/read", s.markNotificationEventRead)
		r.Post("/api/v1/notifications/events/read-all", s.markAllNotificationEventsRead)
	})
}
