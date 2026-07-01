package main

import "testing"

func TestPickProvider(t *testing.T) {
	cases := []struct {
		colima, desktop bool
		want            provider
	}{
		{true, true, provColima},   // colima wins over desktop
		{true, false, provColima},  // colima only
		{false, true, provDesktop}, // desktop only
		{false, false, provInstall}, // nothing -> install path
	}
	for _, c := range cases {
		if got := pickProvider(c.colima, c.desktop); got != c.want {
			t.Errorf("pickProvider(colima=%v, desktop=%v) = %d, want %d",
				c.colima, c.desktop, got, c.want)
		}
	}
}
