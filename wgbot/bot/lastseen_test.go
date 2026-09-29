package bot

import (
	"testing"
	"time"
)

func TestLastSeenText(t *testing.T) {
	cases := []struct {
		name string
		t    time.Time
		want string
	}{
		{"zero", time.Time{}, "никогда"},
		{"now", time.Now().Add(-10 * time.Second), "сейчас онлайн"},
		{"minutes", time.Now().Add(-5 * time.Minute), "был онлайн 5м назад"},
		{"hours", time.Now().Add(-2 * time.Hour), "был онлайн 2ч назад"},
		{"hours+min", time.Now().Add(-145 * time.Minute), "был онлайн 2ч 25м назад"},
		{"days", time.Now().Add(-72 * time.Hour), "был онлайн 3д назад"},
		{"days+hours", time.Now().Add(-26 * time.Hour), "был онлайн 1д 2ч назад"},
	}
	for _, c := range cases {
		if got := lastSeenText(c.t); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
