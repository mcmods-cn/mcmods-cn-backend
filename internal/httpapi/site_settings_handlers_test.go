package httpapi

import "testing"

func TestNormalizeSiteGeneralConfig(t *testing.T) {
	tests := []struct {
		name    string
		input   siteGeneralConfig
		want    siteGeneralConfig
		wantErr bool
	}{
		{name: "defaults are trimmed", input: siteGeneralConfig{SiteName: "  Mcmods-cn  ", LogoURL: " https://cdn.example.com/logo.png "}, want: siteGeneralConfig{SiteName: "Mcmods-cn", LogoURL: "https://cdn.example.com/logo.png"}},
		{name: "empty logo is allowed", input: siteGeneralConfig{SiteName: "测试站点"}, want: siteGeneralConfig{SiteName: "测试站点"}},
		{name: "empty name", input: siteGeneralConfig{}, wantErr: true},
		{name: "unsafe logo scheme", input: siteGeneralConfig{SiteName: "Mcmods-cn", LogoURL: "javascript:alert(1)"}, wantErr: true},
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
