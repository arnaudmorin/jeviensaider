package main

import (
	"testing"

	"benevole-ape/internal/web"
)

func TestMailLimitsFromEnv(t *testing.T) {
	for _, k := range []string{
		"CREATED_PER_ADDRESS_PER_DAY", "CREATED_PER_HOUR", "SIGNUPS_PER_EVENT_PER_HOUR", "PER_HOUR",
		"RECOVER_PER_ADDRESS_PER_DAY", "RECOVER_PER_ADDRESS_PER_CLIENT_PER_DAY", "RECOVER_PER_HOUR", "RECOVER_PER_IP_PER_HOUR",
	} {
		t.Setenv("BENEVOLE_MAIL_"+k, "")
	}
	if l, err := mailLimitsFromEnv(); err != nil || l != web.DefaultMailLimits() {
		t.Errorf("defaults: %+v %v", l, err)
	}
	t.Setenv("BENEVOLE_MAIL_CREATED_PER_ADDRESS_PER_DAY", "5")
	t.Setenv("BENEVOLE_MAIL_SIGNUPS_PER_EVENT_PER_HOUR", "unlimited")
	t.Setenv("BENEVOLE_MAIL_PER_HOUR", " 1000 ")
	t.Setenv("BENEVOLE_MAIL_RECOVER_PER_ADDRESS_PER_DAY", "2")
	t.Setenv("BENEVOLE_MAIL_RECOVER_PER_ADDRESS_PER_CLIENT_PER_DAY", "2")
	t.Setenv("BENEVOLE_MAIL_RECOVER_PER_HOUR", "off")
	t.Setenv("BENEVOLE_MAIL_RECOVER_PER_IP_PER_HOUR", "7")
	l, err := mailLimitsFromEnv()
	want := web.DefaultMailLimits()
	want.CreatedPerAddressPerDay = 5
	want.SignupsPerEventPerHour = 0
	want.PerHour = 1000
	want.RecoverPerAddressPerDay = 2
	want.RecoverPerAddressPerClientPerDay = 2
	want.RecoverPerHour = 0
	want.RecoverPerIPPerHour = 7
	if err != nil || l != want {
		t.Errorf("custom: %+v %v", l, err)
	}
	if d := web.DefaultMailLimits(); d.RecoverPerAddressPerDay != 3 || d.RecoverPerAddressPerClientPerDay != 1 || d.RecoverPerHour != 30 || d.RecoverPerIPPerHour != 10 {
		t.Errorf("default recovery limits: %+v", d)
	}
	for _, bad := range []string{"-1", "lots", "3.5"} {
		t.Setenv("BENEVOLE_MAIL_CREATED_PER_HOUR", bad)
		if _, err := mailLimitsFromEnv(); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
