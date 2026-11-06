package auth

import "testing"

func TestFirstPasswordChangeRouteBoundary(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		allowed      bool
	}{
		{"GET", "/getInfo", true}, {"GET", "/getRouters", true}, {"GET", "/system/user/profile", true},
		{"PUT", "/system/user/profile/updatePwd", true}, {"POST", "/logout", true},
		{"GET", "/demo/item/list", false}, {"POST", "/system/token", false}, {"PUT", "/system/user/profile", false},
		{"PUT", "/system/user/profile/updatePwd/preview", false}, {"GET", "/getInfo/anything", false},
	} {
		if got := passwordChangeRoute(tc.method, tc.path); got != tc.allowed {
			t.Fatalf("%s %s: got %v", tc.method, tc.path, got)
		}
	}
}
