package bot

import (
	"fmt"
	"time"
)

// lastSeenText formats the time since the last handshake in a human-readable way.
func lastSeenText(t time.Time) string {
	if t.IsZero() {
		return "никогда"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "сейчас онлайн"
	case d < time.Hour:
		return fmt.Sprintf("был онлайн %dм назад", int(d.Minutes()))
	case d < 24*time.Hour:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m == 0 {
			return fmt.Sprintf("был онлайн %dч назад", h)
		}
		return fmt.Sprintf("был онлайн %dч %dм назад", h, m)
	default:
		days := int(d.Hours()) / 24
		h := int(d.Hours()) % 24
		if h == 0 {
			return fmt.Sprintf("был онлайн %dд назад", days)
		}
		return fmt.Sprintf("был онлайн %dд %dч назад", days, h)
	}
}
