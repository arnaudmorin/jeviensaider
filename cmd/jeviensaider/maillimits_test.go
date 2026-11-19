package main

import (
	"testing"

	"jeviensaider/internal/web"
)

func TestMailLimitsFromEnv(t *testing.T) {
	for _, k := range []string{"CREATED_PER_ADDRESS_PER_DAY", "CREATED_PER_HOUR", "SIGNUPS_PER_EVENT_PER_HOUR", "PER_HOUR"} {
		t.Setenv("JEVIENSAIDER_MAIL_"+k, "")
	}
	if l, err := mailLimitsFromEnv(); err != nil || l != web.DefaultMailLimits() {
		t.Errorf("defaults: %+v %v", l, err)
	}
	t.Setenv("JEVIENSAIDER_MAIL_CREATED_PER_ADDRESS_PER_DAY", "5")
	t.Setenv("JEVIENSAIDER_MAIL_SIGNUPS_PER_EVENT_PER_HOUR", "unlimited")
	t.Setenv("JEVIENSAIDER_MAIL_PER_HOUR", " 1000 ")
	l, err := mailLimitsFromEnv()
	want := web.MailLimits{CreatedPerAddressPerDay: 5, CreatedPerHour: 30, SignupsPerEventPerHour: 0, PerHour: 1000}
	if err != nil || l != want {
		t.Errorf("custom: %+v %v", l, err)
	}
	for _, bad := range []string{"-1", "lots", "3.5"} {
		t.Setenv("JEVIENSAIDER_MAIL_CREATED_PER_HOUR", bad)
		if _, err := mailLimitsFromEnv(); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
