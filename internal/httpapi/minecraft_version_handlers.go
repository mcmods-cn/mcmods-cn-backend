package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
)

const minecraftVersionsSettingKey = "minecraft.versions"

type minecraftVersionOption struct {
	Code string `json:"code"`
	Type string `json:"type"`
}

type minecraftLoaderOption struct {
	Code     string   `json:"code"`
	Name     string   `json:"name"`
	Versions []string `json:"versions"`
}

type minecraftVersionConfig struct {
	Versions []minecraftVersionOption `json:"versions"`
	Loaders  []minecraftLoaderOption  `json:"loaders"`
}

func (s *Server) publicMinecraftVersions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.minecraftVersionConfig(r))
}

func (s *Server) updateMinecraftVersions(w http.ResponseWriter, r *http.Request) {
	var payload minecraftVersionConfig
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	config, err := normalizeMinecraftVersionConfig(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	raw, _ := json.Marshal(config)
	_, err = s.db.Exec(
		r.Context(),
		`insert into system_settings (key, value, updated_at) values ($1, $2::jsonb, now())
		 on conflict (key) do update set value=excluded.value, updated_at=now()`,
		minecraftVersionsSettingKey,
		string(raw),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存 Minecraft 版本设置失败")
		return
	}
	writeJSON(w, http.StatusOK, config)
}

func (s *Server) minecraftVersionConfig(r *http.Request) minecraftVersionConfig {
	config := defaultMinecraftVersionConfig()
	var raw []byte
	if err := s.db.QueryRow(r.Context(), `select value from system_settings where key=$1`, minecraftVersionsSettingKey).Scan(&raw); err == nil {
		var stored minecraftVersionConfig
		if json.Unmarshal(raw, &stored) == nil {
			if normalized, normalizeErr := normalizeMinecraftVersionConfig(stored); normalizeErr == nil {
				config = mergeMinecraftVersionConfig(config, normalized)
			}
		}
	}
	return config
}

func mergeMinecraftVersionConfig(base minecraftVersionConfig, stored minecraftVersionConfig) minecraftVersionConfig {
	index := make(map[string]int, len(base.Versions))
	for position, version := range base.Versions {
		index[version.Code] = position
	}
	for _, version := range stored.Versions {
		if position, ok := index[version.Code]; ok {
			base.Versions[position] = version
			continue
		}
		index[version.Code] = len(base.Versions)
		base.Versions = append(base.Versions, version)
	}
	if len(stored.Loaders) > 0 {
		base.Loaders = stored.Loaders
	}
	return base
}

func normalizeMinecraftVersionConfig(payload minecraftVersionConfig) (minecraftVersionConfig, error) {
	versionSet := map[string]bool{}
	versions := make([]minecraftVersionOption, 0, len(payload.Versions))
	for _, version := range payload.Versions {
		version.Code = strings.TrimSpace(version.Code)
		version.Type = strings.TrimSpace(version.Type)
		if version.Code == "" || versionSet[version.Code] {
			continue
		}
		if len(version.Code) > 80 {
			return minecraftVersionConfig{}, &requestError{message: "Minecraft 版本名称过长"}
		}
		if version.Type != "release" && version.Type != "snapshot" && version.Type != "april_fools" && version.Type != "legacy" {
			version.Type = "release"
		}
		versionSet[version.Code] = true
		versions = append(versions, version)
	}
	if len(versions) > 500 {
		return minecraftVersionConfig{}, &requestError{message: "Minecraft 版本数量过多"}
	}
	loaderSet := map[string]bool{}
	loaders := make([]minecraftLoaderOption, 0, len(payload.Loaders))
	for _, loader := range payload.Loaders {
		loader.Code = strings.TrimSpace(loader.Code)
		loader.Name = strings.TrimSpace(loader.Name)
		if loader.Code == "" || loaderSet[loader.Code] {
			continue
		}
		if len(loader.Code) > 80 || len(loader.Name) > 120 {
			return minecraftVersionConfig{}, &requestError{message: "模组加载器名称过长"}
		}
		loaderSet[loader.Code] = true
		loader.Versions = uniqueTrimmed(loader.Versions, 500)
		filtered := loader.Versions[:0]
		for _, version := range loader.Versions {
			if versionSet[version] {
				filtered = append(filtered, version)
			}
		}
		loader.Versions = filtered
		if loader.Name == "" {
			loader.Name = loader.Code
		}
		loaders = append(loaders, loader)
	}
	if len(loaders) > 100 {
		return minecraftVersionConfig{}, &requestError{message: "模组加载器数量过多"}
	}
	return minecraftVersionConfig{Versions: versions, Loaders: loaders}, nil
}

func defaultMinecraftVersionConfig() minecraftVersionConfig {
	releases := []string{
		"1.21.5", "1.21.4", "1.21.3", "1.21.2", "1.21.1", "1.21", "1.20.6", "1.20.5", "1.20.4", "1.20.3", "1.20.2", "1.20.1", "1.20",
		"1.19.4", "1.19.3", "1.19.2", "1.19.1", "1.19", "1.18.2", "1.18.1", "1.18", "1.17.1", "1.17", "1.16.5", "1.16.4", "1.16.3", "1.16.2", "1.16.1", "1.16",
		"1.15.2", "1.15.1", "1.15", "1.14.4", "1.14.3", "1.14.2", "1.14.1", "1.14", "1.13.2", "1.13.1", "1.13", "1.12.2", "1.12.1", "1.12", "1.11.2", "1.11.1", "1.11",
		"1.10.2", "1.10.1", "1.10", "1.9.4", "1.9.3", "1.9.2", "1.9.1", "1.9", "1.8.9", "1.8.8", "1.8.7", "1.8.6", "1.8.5", "1.8.4", "1.8.3", "1.8.2", "1.8.1", "1.8",
		"1.7.10", "1.7.9", "1.7.8", "1.7.7", "1.7.6", "1.7.5", "1.7.4", "1.7.3", "1.7.2", "1.6.4", "1.6.2", "1.6.1", "1.5.2", "1.5.1", "1.5", "1.4.7", "1.4.6", "1.4.5", "1.4.4", "1.4.2", "1.3.2", "1.3.1", "1.2.5", "1.2.4", "1.2.3", "1.2.2", "1.2.1", "1.1", "1.0.1", "1.0.0",
	}
	snapshots := []string{"25w14craftmine", "25w10a", "25w09b", "25w09a", "25w08a", "25w07a", "25w06a", "25w05a", "25w04a", "25w03a", "25w02a", "24w46a", "24w45a", "24w44a", "24w40a", "24w39a", "24w38a", "24w37a", "24w36a", "24w35a", "24w34a", "24w33a", "24w21b", "24w21a", "24w20a", "24w19b", "24w19a", "24w18a", "23w51b", "23w51a", "23w46a", "23w45a", "23w44a", "23w43b", "23w43a", "23w42a", "23w41a", "23w40a", "23w35a", "23w33a", "23w32a", "23w31a"}
	aprilFools := []string{"25w14craftmine", "24w14potato", "23w13a_or_b", "22w13oneBlockAtATime", "20w14∞", "3D Shareware v1.34", "1.RV-Pre1", "15w14a", "Minecraft 2.0"}
	legacy := []string{"b1.8.1", "b1.7.3", "b1.6.6", "b1.5_01", "b1.4_01", "b1.3_01", "b1.2_02", "b1.1_02", "a1.2.6", "a1.2.5", "a1.2.4_01", "a1.2.3_04", "a1.2.2b", "a1.2.1_01", "a1.2.0_02", "a1.1.2_01", "a1.1.0", "a1.0.17_04", "a1.0.16", "a1.0.15", "a1.0.14", "a1.0.11", "a1.0.5_01", "inf-20100630", "c0.30_01c", "c0.0.23a_01", "rd-132211"}
	versions := make([]minecraftVersionOption, 0, len(releases)+len(snapshots)+len(aprilFools)+len(legacy))
	seen := map[string]bool{}
	appendVersions := func(codes []string, versionType string) {
		for _, code := range codes {
			if seen[code] {
				continue
			}
			seen[code] = true
			versions = append(versions, minecraftVersionOption{Code: code, Type: versionType})
		}
	}
	appendVersions(releases, "release")
	appendVersions(aprilFools, "april_fools")
	appendVersions(snapshots, "snapshot")
	appendVersions(legacy, "legacy")
	versionCodes := make([]string, 0, len(versions))
	for _, version := range versions {
		versionCodes = append(versionCodes, version.Code)
	}
	loaderNames := []string{"Fabric", "Forge", "NeoForge", "Babric", "BTA (Babric)", "Java Agent", "Legacy Fabric", "LiteLoader", "Risugami's ModLoader", "NilLoader", "Ornithe", "Quilt", "Rift"}
	loaders := make([]minecraftLoaderOption, 0, len(loaderNames))
	for _, name := range loaderNames {
		loaders = append(loaders, minecraftLoaderOption{Code: name, Name: name, Versions: append([]string(nil), versionCodes...)})
	}
	return minecraftVersionConfig{Versions: versions, Loaders: loaders}
}

func compatibilitySummary(items []modLoaderCompatibilityPayload) ([]string, []string) {
	loaderSet := map[string]bool{}
	versionSet := map[string]bool{}
	loaders := make([]string, 0, len(items))
	versions := make([]string, 0)
	for _, item := range items {
		if !loaderSet[item.Loader] {
			loaderSet[item.Loader] = true
			loaders = append(loaders, item.Loader)
		}
		for _, version := range item.Versions {
			if !versionSet[version] {
				versionSet[version] = true
				versions = append(versions, version)
			}
		}
	}
	return loaders, versions
}
