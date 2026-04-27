package loop

// DeliveryTarget specifies where to deliver loop/cron results.
type DeliveryTarget struct {
	ChannelType string `json:"channel_type"` // e.g., "telegram", "slack"
	ChannelID   string `json:"channel_id"`   // platform-specific destination
}

// NeedsDelivery returns true if a delivery target is configured.
func (d *DeliveryTarget) NeedsDelivery() bool {
	return d != nil && d.ChannelType != "" && d.ChannelID != ""
}
