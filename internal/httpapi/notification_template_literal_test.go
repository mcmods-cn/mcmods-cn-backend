package httpapi

import "testing"

func TestOCT02NotificationTemplateValuesRemainLiteral(t *testing.T) {
	configuration := notificationTemplateConfig{Templates: []notificationTemplateDefinition{{Code: "literal", Version: 1, Translations: map[string]localizedNotificationTemplate{"en-US": {Title: "{name}", Body: "Reason: {reason}; repeated: {name}"}}}}}
	values := map[string]string{"name": "Mod {reason} {unknown}", "reason": "Keep {name} literal"}
	for i := 0; i < 100; i++ {
		rendered, err := renderNotificationTemplateForLocale(configuration, "en-US", "literal", values)
		if err != nil {
			t.Fatal(err)
		}
		if rendered.Title != values["name"] || rendered.Body != "Reason: "+values["reason"]+"; repeated: "+values["name"] {
			t.Fatalf("values were reinterpreted: %#v", rendered)
		}
	}
	if _, err := renderNotificationTemplateForLocale(configuration, "en-US", "literal", map[string]string{"name": "ok"}); err == nil {
		t.Fatal("missing original template parameter accepted")
	}
}
