package httpapi

import "testing"

func TestNormalizeSiteGeneralConfig(t *testing.T) {
	tests := []struct {
		name    string
		input   siteGeneralConfig
		want    siteGeneralConfig
		wantErr bool
	}{
		{name: "safe shared PNG logo is trimmed", input: siteGeneralConfig{SiteName: "  Mcmods-cn  ", LogoURL: " /site-assets/site-logo-abc123xyz.png "}, want: siteGeneralConfig{SiteName: "Mcmods-cn", LogoURL: "/site-assets/site-logo-abc123xyz.png"}},
		{name: "obsolete instance-local derivative", input: siteGeneralConfig{SiteName: "Mcmods-cn", LogoURL: "/site-assets/site-logo-0123456789abcdefabcd.webp"}, wantErr: true},
		{name: "empty logo is allowed", input: siteGeneralConfig{SiteName: "测试站点"}, want: siteGeneralConfig{SiteName: "测试站点"}},
		{name: "empty name", input: siteGeneralConfig{}, wantErr: true},
		{name: "external logo", input: siteGeneralConfig{SiteName: "Mcmods-cn", LogoURL: "https://cdn.example.com/logo.png"}, wantErr: true},
		{name: "unmanaged public path", input: siteGeneralConfig{SiteName: "Mcmods-cn", LogoURL: "/other/logo.png"}, wantErr: true},
		{name: "legacy raw logo", input: siteGeneralConfig{SiteName: "Mcmods-cn", LogoURL: "/site-assets/site-logo-0123456789abcdefabcd.png"}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := test.input
			err := normalizeSiteGeneralConfig(&actual)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalize: %v", err)
			}
			if actual != test.want {
				t.Fatalf("got %#v, want %#v", actual, test.want)
			}
		})
	}
}
