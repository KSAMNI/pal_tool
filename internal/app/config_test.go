package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestParseOptionSettingsKeepsQuotedAndNestedCommas(t *testing.T) {
	order, raw, err := parseOptionSettings(`ServerName="A, B",CrossplayPlatforms=(Steam,Xbox),RESTAPIEnabled=True`)
	if err != nil {
		t.Fatalf("parseOptionSettings() error = %v", err)
	}
	wantOrder := []string{"ServerName", "CrossplayPlatforms", "RESTAPIEnabled"}
	if !reflect.DeepEqual(order, wantOrder) {
		t.Fatalf("order = %#v, want %#v", order, wantOrder)
	}
	if got := raw["ServerName"]; got != `"A, B"` {
		t.Fatalf("ServerName = %q", got)
	}
	if got := raw["CrossplayPlatforms"]; got != `(Steam,Xbox)` {
		t.Fatalf("CrossplayPlatforms = %q", got)
	}
}

func TestPalConfigLoadSaveCreatesBackupAndPreservesUnknownFields(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	defaultPath := filepath.Join(serverPath, "DefaultPalWorldSettings.ini")
	defaultContent := `[/Script/Pal.PalGameWorldSettings]
OptionSettings=(ServerName="Original",ServerPlayerMaxNum=16,BaseCampMaxNumInGuild=4,MaxBuildingLimitNum=0,ServerReplicatePawnCullDistance=15000.000000,RESTAPIEnabled=False,RESTAPIPort=8212,CrossplayPlatforms=(Steam,Xbox),bAllowClientMod=False,bIsPvP=False,bEnablePlayerToPlayerDamage=False,bEnableDefenseOtherGuildPlayer=False,UnknownFutureKey="keep,me")
`
	if err := os.WriteFile(defaultPath, []byte(defaultContent), 0o644); err != nil {
		t.Fatalf("write default config: %v", err)
	}
	if _, err := panel.db.Exec(
		`INSERT INTO app_settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		"pal_server_path",
		serverPath,
	); err != nil {
		t.Fatalf("set pal_server_path: %v", err)
	}

	doc, err := panel.loadPalConfig(true)
	if err != nil {
		t.Fatalf("loadPalConfig() error = %v", err)
	}
	if doc.Values.ServerName != "Original" {
		t.Fatalf("ServerName = %q", doc.Values.ServerName)
	}
	if doc.RawValues["UnknownFutureKey"] != `"keep,me"` {
		t.Fatalf("unknown field was not parsed: %#v", doc.RawValues)
	}

	nextValues := doc.Values
	nextValues.ServerName = "Updated"
	nextValues.ServerPlayerMaxNum = 24
	nextValues.BaseCampMaxNumInGuild = 6
	nextValues.MaxBuildingLimitNum = 8000
	nextValues.ServerReplicatePawnCullDistance = 12000
	nextValues.RESTAPIEnabled = true
	nextValues.AllowClientMod = true
	nextValues.IsPvP = true
	nextValues.EnablePlayerToPlayerDamage = true
	nextValues.EnableDefenseOtherGuildPlayer = true
	raw := doc.rawWithValues(nextValues)
	nextContent := doc.render(raw)
	backupPath, err := backupFile(doc.ConfigPath)
	if err != nil {
		t.Fatalf("backupFile() error = %v", err)
	}
	if err := os.WriteFile(doc.ConfigPath, []byte(nextContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if !fileExists(backupPath) {
		t.Fatalf("backup file does not exist: %s", backupPath)
	}

	saved, err := os.ReadFile(doc.ConfigPath)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	savedText := string(saved)
	for _, want := range []string{
		`ServerName="Updated"`,
		`ServerPlayerMaxNum=24`,
		`BaseCampMaxNumInGuild=6`,
		`MaxBuildingLimitNum=8000`,
		`ServerReplicatePawnCullDistance=12000.000000`,
		`RESTAPIEnabled=True`,
		`CrossplayPlatforms=(Steam,Xbox)`,
		`bAllowClientMod=True`,
		`bIsPvP=True`,
		`bEnablePlayerToPlayerDamage=True`,
		`bEnableDefenseOtherGuildPlayer=True`,
		`UnknownFutureKey="keep,me"`,
	} {
		if !strings.Contains(savedText, want) {
			t.Fatalf("saved config missing %q: %s", want, savedText)
		}
	}
}

func TestPalConfigPerformanceFieldsApplyConservativeLimits(t *testing.T) {
	previous := palConfigValues{
		BaseCampMaxNumInGuild:           4,
		MaxBuildingLimitNum:             0,
		ServerReplicatePawnCullDistance: 15000,
	}
	values := palConfigValues{
		BaseCampMaxNumInGuild:           99,
		MaxBuildingLimitNum:             -1,
		ServerReplicatePawnCullDistance: 20000,
	}

	values.applyDefaultsFrom(previous)

	if values.BaseCampMaxNumInGuild != 10 {
		t.Fatalf("BaseCampMaxNumInGuild = %d, want 10", values.BaseCampMaxNumInGuild)
	}
	if values.MaxBuildingLimitNum != 0 {
		t.Fatalf("MaxBuildingLimitNum = %d, want 0", values.MaxBuildingLimitNum)
	}
	if values.ServerReplicatePawnCullDistance != 15000 {
		t.Fatalf("ServerReplicatePawnCullDistance = %f, want 15000", values.ServerReplicatePawnCullDistance)
	}

	values.ServerReplicatePawnCullDistance = 1000
	values.applyDefaultsFrom(previous)
	if values.ServerReplicatePawnCullDistance != 5000 {
		t.Fatalf("ServerReplicatePawnCullDistance = %f, want 5000", values.ServerReplicatePawnCullDistance)
	}
}

func TestDefaultPalConfigFieldsCoverCurrentOptionSettings(t *testing.T) {
	_, _, optionText, err := findOptionSettings(defaultPalWorldSettings)
	if err != nil {
		t.Fatalf("findOptionSettings() error = %v", err)
	}
	_, raw, err := parseOptionSettings(optionText)
	if err != nil {
		t.Fatalf("parseOptionSettings() error = %v", err)
	}
	known := map[string]bool{}
	for _, key := range knownConfigKeys() {
		if known[key] {
			t.Fatalf("duplicate known config key: %s", key)
		}
		known[key] = true
	}
	for key := range raw {
		if !known[key] {
			t.Fatalf("default config key %s is not represented in knownConfigKeys", key)
		}
	}
}

func TestPalConfigCurrentDefaultFieldsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "PalWorldSettings.ini")
	if err := os.WriteFile(configPath, []byte(defaultPalWorldSettings), 0o644); err != nil {
		t.Fatalf("write default config: %v", err)
	}
	doc, err := readPalConfigDocument(configPath, configPath, "LinuxServer")
	if err != nil {
		t.Fatalf("readPalConfigDocument() error = %v", err)
	}
	if doc.Values.RandomizerType != "None" {
		t.Fatalf("RandomizerType = %q, want None", doc.Values.RandomizerType)
	}
	if !doc.Values.EnableFastTravel {
		t.Fatalf("EnableFastTravel = false, want true")
	}
	if doc.Values.BanListURL != "https://b.palworldgame.com/api/banlist.txt" {
		t.Fatalf("BanListURL = %q", doc.Values.BanListURL)
	}
	unchanged := doc.render(doc.rawWithValues(doc.Values))
	if !strings.Contains(unchanged, `DenyTechnologyList=,GuildRejoinCooldownMinutes=0`) {
		t.Fatalf("unchanged render did not preserve empty DenyTechnologyList as a raw empty value: %s", unchanged)
	}

	nextValues := doc.Values
	nextValues.RandomizerType = "All"
	nextValues.RandomizerSeed = "seed-1337"
	nextValues.IsRandomizerPalLevelRandom = true
	nextValues.PalDamageRateAttack = 2.5
	nextValues.PlayerStomachDecreaseRate = 0.5
	nextValues.EnableFastTravel = false
	nextValues.EggDefaultHatchingTime = 12
	nextValues.DenyTechnologyList = `("PALBOX","RepairBench")`
	nextValues.AdditionalDropItemWhenPlayerKillingInPvPEnabled = true
	nextValues.AllowEnhanceStatAttack = false

	rendered := doc.render(doc.rawWithValues(nextValues))
	for _, want := range []string{
		`RandomizerType=All`,
		`RandomizerSeed="seed-1337"`,
		`bIsRandomizerPalLevelRandom=True`,
		`PalDamageRateAttack=2.500000`,
		`PlayerStomachDecreaceRate=0.500000`,
		`bEnableFastTravel=False`,
		`PalEggDefaultHatchingTime=12.000000`,
		`DenyTechnologyList=("PALBOX","RepairBench")`,
		`bAdditionalDropItemWhenPlayerKillingInPvPMode=True`,
		`bAllowEnhanceStat_Attack=False`,
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered config missing %q: %s", want, rendered)
		}
	}
	if strings.Contains(rendered, "(EggDefaultHatchingTime=") || strings.Contains(rendered, ",EggDefaultHatchingTime=") {
		t.Fatalf("rendered config kept legacy EggDefaultHatchingTime key: %s", rendered)
	}
}

func TestPalConfigLegacyEggHatchingFieldDoesNotRenderLeadingComma(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "PalWorldSettings.ini")
	if err := os.WriteFile(configPath, []byte(`[/Script/Pal.PalGameWorldSettings]
OptionSettings=(EggDefaultHatchingTime=72.000000)
`), 0o644); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}
	doc, err := readPalConfigDocument(configPath, configPath, "LinuxServer")
	if err != nil {
		t.Fatalf("readPalConfigDocument() error = %v", err)
	}
	rendered := doc.render(doc.rawWithValues(doc.Values))
	if strings.Contains(rendered, "OptionSettings=(,") {
		t.Fatalf("rendered config has a leading comma after legacy field migration: %s", rendered)
	}
	if strings.Contains(rendered, "OptionSettings=(EggDefaultHatchingTime=") || strings.Contains(rendered, ",EggDefaultHatchingTime=") {
		t.Fatalf("rendered config kept legacy EggDefaultHatchingTime key: %s", rendered)
	}
	if !strings.Contains(rendered, "PalEggDefaultHatchingTime=72.000000") {
		t.Fatalf("rendered config missing current PalEggDefaultHatchingTime key: %s", rendered)
	}
}

func TestAtomicWriteFileFailedReplaceKeepsOriginalAndRemovesTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "PalWorldSettings.ini")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatalf("write original: %v", err)
	}

	previousReplace := atomicReplaceFile
	atomicReplaceFile = func(src, dst string) error {
		if _, err := os.Stat(src); err != nil {
			t.Fatalf("replacement source missing before injected failure: %v", err)
		}
		return os.ErrPermission
	}
	defer func() {
		atomicReplaceFile = previousReplace
	}()

	err := atomicWriteFile(path, []byte("updated"), 0o644)
	if err == nil {
		t.Fatal("atomicWriteFile() error = nil, want injected replacement failure")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read original after failed replace: %v", err)
	}
	if string(content) != "original" {
		t.Fatalf("config content after failed replace = %q, want original", string(content))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read config dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".PalWorldSettings.ini.tmp-") {
			t.Fatalf("temporary atomic write file was not removed: %s", entry.Name())
		}
	}
}

func TestCopyFileKeepsConfigLimitSeparateAndPreservesContent(t *testing.T) {
	setPalConfigFileLimit(t, 1)
	dir := t.TempDir()
	src := filepath.Join(dir, "source.bin")
	dst := filepath.Join(dir, "target.bin")
	content := strings.Repeat("x", 4096)
	if err := os.WriteFile(src, []byte(content), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile() error = %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read copied file: %v", err)
	}
	if string(got) != content {
		t.Fatalf("copied content mismatch")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(dst)
		if err != nil {
			t.Fatalf("stat copied file: %v", err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("copied permissions = %v, want 0600", info.Mode().Perm())
		}
	}
}

func TestCopyFileFailedReplaceKeepsOriginalAndRemovesTemp(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source.bin")
	dst := filepath.Join(dir, "target.bin")
	if err := os.WriteFile(src, []byte("updated"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(dst, []byte("original"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	previousReplace := atomicReplaceFile
	atomicReplaceFile = func(src, dst string) error {
		if _, err := os.Stat(src); err != nil {
			t.Fatalf("replacement source missing before injected failure: %v", err)
		}
		return os.ErrPermission
	}
	defer func() {
		atomicReplaceFile = previousReplace
	}()

	err := copyFile(src, dst)
	if err == nil {
		t.Fatal("copyFile() error = nil, want injected replacement failure")
	}
	content, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read target after failed replace: %v", err)
	}
	if string(content) != "original" {
		t.Fatalf("target content after failed replace = %q, want original", string(content))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read target dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".target.bin.tmp-") {
			t.Fatalf("temporary copy file was not removed: %s", entry.Name())
		}
	}
}

func TestConfigRoutes(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	if _, err := panel.db.Exec(
		`INSERT INTO app_settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		"pal_server_path",
		serverPath,
	); err != nil {
		t.Fatalf("set pal_server_path: %v", err)
	}

	server := httptest.NewServer(panel.Routes())
	defer server.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}
	client := &http.Client{Jar: jar}

	setup := map[string]string{"username": "admin", "password": "password123"}
	resp := doJSON(t, client, http.MethodPost, server.URL+"/api/auth/setup", setup)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = doJSON(t, client, http.MethodPost, server.URL+"/api/config/init", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/config/init status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = doJSON(t, client, http.MethodGet, server.URL+"/api/config", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/config status = %d", resp.StatusCode)
	}
	var config palConfigPayload
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	resp.Body.Close()
	config.Values.ServerName = "Route Saved"
	config.Values.BaseCampMaxNumInGuild = 7
	config.Values.MaxBuildingLimitNum = 12000
	config.Values.ServerReplicatePawnCullDistance = 11000
	config.Values.RESTAPIEnabled = true
	config.Values.AllowClientMod = true
	config.Values.IsPvP = true
	config.Values.EnablePlayerToPlayerDamage = true
	config.Values.EnableDefenseOtherGuildPlayer = true

	resp = doJSON(t, client, http.MethodPut, server.URL+"/api/config", map[string]palConfigValues{"values": config.Values})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /api/config status = %d", resp.StatusCode)
	}
	var saved palConfigPayload
	if err := json.NewDecoder(resp.Body).Decode(&saved); err != nil {
		t.Fatalf("decode saved config: %v", err)
	}
	resp.Body.Close()
	if saved.Values.ServerName != "Route Saved" ||
		saved.Values.BaseCampMaxNumInGuild != 7 ||
		saved.Values.MaxBuildingLimitNum != 12000 ||
		saved.Values.ServerReplicatePawnCullDistance != 11000 ||
		!saved.Values.RESTAPIEnabled ||
		!saved.Values.AllowClientMod ||
		!saved.Values.IsPvP ||
		!saved.Values.EnablePlayerToPlayerDamage ||
		!saved.Values.EnableDefenseOtherGuildPlayer ||
		saved.BackupPath == "" ||
		!saved.NeedsRestart {
		t.Fatalf("unexpected saved response: %#v", saved)
	}
}

func TestConfigGetMissingDoesNotInitialize(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	setTestAppSetting(t, panel, "pal_server_path", serverPath)
	configPath, _, _, err := palConfigPaths(serverPath)
	if err != nil {
		t.Fatalf("palConfigPaths() error = %v", err)
	}

	server, client := newAuthenticatedTestServer(t, panel)
	resp := doJSON(t, client, http.MethodGet, server.URL+"/api/config", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /api/config status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
	if fileExists(configPath) {
		t.Fatalf("GET /api/config initialized missing config: %s", configPath)
	}
	assertNoTasks(t, panel)
}

func TestConfigGetEmptyConfigReturnsNotInitialized(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	setTestAppSetting(t, panel, "pal_server_path", serverPath)
	configPath, _, _, err := palConfigPaths(serverPath)
	if err != nil {
		t.Fatalf("palConfigPaths() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(configPath, nil, 0o644); err != nil {
		t.Fatalf("write empty config: %v", err)
	}

	server, client := newAuthenticatedTestServer(t, panel)
	resp := doJSON(t, client, http.MethodGet, server.URL+"/api/config", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /api/config status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if len(content) != 0 {
		t.Fatalf("GET /api/config unexpectedly rewrote empty config: %q", string(content))
	}
	assertNoTasks(t, panel)
}

func TestConfigInitRepairsEmptyConfigFromDefault(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	setTestAppSetting(t, panel, "pal_server_path", serverPath)
	configPath, defaultPath, _, err := palConfigPaths(serverPath)
	if err != nil {
		t.Fatalf("palConfigPaths() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	defaultContent := `[/Script/Pal.PalGameWorldSettings]
OptionSettings=(ServerName="Recovered From Default",ServerPlayerMaxNum=16)
`
	if err := os.WriteFile(defaultPath, []byte(defaultContent), 0o644); err != nil {
		t.Fatalf("write default config: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("\n"), 0o644); err != nil {
		t.Fatalf("write empty config shell: %v", err)
	}

	server, client := newAuthenticatedTestServer(t, panel)
	resp := doJSON(t, client, http.MethodPost, server.URL+"/api/config/init", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/config/init status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var payload palConfigPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	resp.Body.Close()
	if payload.Values.ServerName != "Recovered From Default" {
		t.Fatalf("ServerName = %q, want %q", payload.Values.ServerName, "Recovered From Default")
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read repaired config: %v", err)
	}
	if string(content) != defaultContent {
		t.Fatalf("repaired config = %q, want copied default content", string(content))
	}
	assertNoTasks(t, panel)
}

func TestConfigGetRejectsOversizedConfig(t *testing.T) {
	setPalConfigFileLimit(t, 128)
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	setTestAppSetting(t, panel, "pal_server_path", serverPath)
	configPath, _, _, err := palConfigPaths(serverPath)
	if err != nil {
		t.Fatalf("palConfigPaths() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	oversized := `[/Script/Pal.PalGameWorldSettings]
OptionSettings=(ServerName="` + strings.Repeat("x", 160) + `")
`
	if err := os.WriteFile(configPath, []byte(oversized), 0o644); err != nil {
		t.Fatalf("write oversized config: %v", err)
	}

	server, client := newAuthenticatedTestServer(t, panel)
	resp := doJSON(t, client, http.MethodGet, server.URL+"/api/config", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("GET /api/config status = %d, want %d", resp.StatusCode, http.StatusRequestEntityTooLarge)
	}
	assertNoTasks(t, panel)
}

func TestConfigInitRejectsOversizedDefaultWithoutMutation(t *testing.T) {
	setPalConfigFileLimit(t, 128)
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	setTestAppSetting(t, panel, "pal_server_path", serverPath)
	configPath, defaultPath, _, err := palConfigPaths(serverPath)
	if err != nil {
		t.Fatalf("palConfigPaths() error = %v", err)
	}
	if err := os.WriteFile(defaultPath, []byte(strings.Repeat("x", 160)), 0o644); err != nil {
		t.Fatalf("write oversized default config: %v", err)
	}

	server, client := newAuthenticatedTestServer(t, panel)
	resp := doJSON(t, client, http.MethodPost, server.URL+"/api/config/init", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("POST /api/config/init status = %d, want %d", resp.StatusCode, http.StatusRequestEntityTooLarge)
	}
	if fileExists(configPath) {
		t.Fatalf("oversized default initialized config: %s", configPath)
	}
	assertNoTasks(t, panel)
}

func TestBackupFileRejectsOversizedConfigWithoutBak(t *testing.T) {
	setPalConfigFileLimit(t, 64)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "PalWorldSettings.ini")
	if err := os.WriteFile(configPath, []byte(strings.Repeat("x", 96)), 0o644); err != nil {
		t.Fatalf("write oversized config: %v", err)
	}

	_, err := backupFile(configPath)
	if !errors.Is(err, errPalConfigFileTooLarge) {
		t.Fatalf("backupFile() error = %v, want errPalConfigFileTooLarge", err)
	}
	assertNoConfigBakFiles(t, configPath)
}

func TestConfigPutRejectsOversizedRenderedConfigWithoutBackupOrBak(t *testing.T) {
	setPalConfigFileLimit(t, 256)
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	setTestAppSetting(t, panel, "pal_server_path", serverPath)
	configPath, _, _, err := palConfigPaths(serverPath)
	if err != nil {
		t.Fatalf("palConfigPaths() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	original := `[/Script/Pal.PalGameWorldSettings]
OptionSettings=(ServerName="Original",ServerPlayerMaxNum=16)
`
	if int64(len(original)) >= maxPalConfigFileBytes {
		t.Fatalf("test original length %d must stay under limit %d", len(original), maxPalConfigFileBytes)
	}
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	server, client := newAuthenticatedTestServer(t, panel)
	resp := doJSON(t, client, http.MethodPut, server.URL+"/api/config", map[string]palConfigValues{
		"values": {ServerName: strings.Repeat("x", 160), ServerPlayerMaxNum: 16},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("PUT /api/config status = %d, want %d", resp.StatusCode, http.StatusRequestEntityTooLarge)
	}
	saved, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(saved) != original {
		t.Fatalf("config was mutated:\n%s", string(saved))
	}
	assertNoBackups(t, panel)
	assertNoConfigBakFiles(t, configPath)
	assertNoTasks(t, panel)
}

func TestConfigPutRejectsExternalServerWithoutMutation(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	if _, err := panel.db.Exec(
		`INSERT INTO app_settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		"pal_server_path",
		serverPath,
	); err != nil {
		t.Fatalf("set pal_server_path: %v", err)
	}
	configPath, _, _, err := palConfigPaths(serverPath)
	if err != nil {
		t.Fatalf("palConfigPaths() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	original := `[/Script/Pal.PalGameWorldSettings]
OptionSettings=(ServerName="Original",ServerPlayerMaxNum=16)
`
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	detectorCalled := false
	panel.serverProcessDetector = func(settings settingsPayload) (bool, error) {
		detectorCalled = true
		if settings.PalServerPath != serverPath {
			t.Fatalf("detector PalServerPath = %q, want %q", settings.PalServerPath, serverPath)
		}
		return true, nil
	}

	server := httptest.NewServer(panel.Routes())
	defer server.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}
	client := &http.Client{Jar: jar}

	setup := map[string]string{"username": "admin", "password": "password123"}
	resp := doJSON(t, client, http.MethodPost, server.URL+"/api/auth/setup", setup)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	nextValues := palConfigValues{ServerName: "Changed", ServerPlayerMaxNum: 32}
	resp = doJSON(t, client, http.MethodPut, server.URL+"/api/config", map[string]palConfigValues{"values": nextValues})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("PUT /api/config status = %d, want %d", resp.StatusCode, http.StatusConflict)
	}
	resp.Body.Close()
	if !detectorCalled {
		t.Fatal("serverProcessDetector was not called")
	}

	saved, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(saved) != original {
		t.Fatalf("config was mutated:\n%s", string(saved))
	}
	backups, err := panel.listBackups()
	if err != nil {
		t.Fatalf("listBackups() error = %v", err)
	}
	if len(backups) != 0 {
		t.Fatalf("backups = %#v, want none", backups)
	}
	entries, err := os.ReadDir(filepath.Dir(configPath))
	if err != nil {
		t.Fatalf("read config dir: %v", err)
	}
	backupPrefix := filepath.Base(configPath) + "."
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, backupPrefix) && strings.HasSuffix(name, ".bak") {
			t.Fatalf("unexpected config backup file created: %s", name)
		}
	}
}

func TestConfigInitRejectsRunningTaskWithoutMutation(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	setTestAppSetting(t, panel, "pal_server_path", serverPath)
	configPath, _, _, err := palConfigPaths(serverPath)
	if err != nil {
		t.Fatalf("palConfigPaths() error = %v", err)
	}

	panel.taskMu.Lock()
	panel.taskRunning = true
	panel.taskMu.Unlock()
	defer func() {
		panel.taskMu.Lock()
		panel.taskRunning = false
		panel.taskMu.Unlock()
	}()

	server, client := newAuthenticatedTestServer(t, panel)
	resp := doJSON(t, client, http.MethodPost, server.URL+"/api/config/init", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("POST /api/config/init status = %d, want %d", resp.StatusCode, http.StatusConflict)
	}
	if fileExists(configPath) {
		t.Fatalf("config was initialized despite active task: %s", configPath)
	}
	assertNoTasks(t, panel)
}

func TestConfigPutRejectsRunningTaskWithoutMutation(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	setTestAppSetting(t, panel, "pal_server_path", serverPath)
	configPath, _, _, err := palConfigPaths(serverPath)
	if err != nil {
		t.Fatalf("palConfigPaths() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	original := `[/Script/Pal.PalGameWorldSettings]
OptionSettings=(ServerName="Original",ServerPlayerMaxNum=16)
`
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	panel.taskMu.Lock()
	panel.taskRunning = true
	panel.taskMu.Unlock()
	defer func() {
		panel.taskMu.Lock()
		panel.taskRunning = false
		panel.taskMu.Unlock()
	}()

	server, client := newAuthenticatedTestServer(t, panel)
	resp := doJSON(t, client, http.MethodPut, server.URL+"/api/config", map[string]palConfigValues{
		"values": {ServerName: "Changed", ServerPlayerMaxNum: 32},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("PUT /api/config status = %d, want %d", resp.StatusCode, http.StatusConflict)
	}
	saved, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(saved) != original {
		t.Fatalf("config was mutated:\n%s", string(saved))
	}
	assertNoBackups(t, panel)
	assertNoConfigBakFiles(t, configPath)
	assertNoTasks(t, panel)
}

func TestConfigBackupRejectsRunningTaskWithoutBak(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	setTestAppSetting(t, panel, "pal_server_path", serverPath)
	configPath, _, _, err := palConfigPaths(serverPath)
	if err != nil {
		t.Fatalf("palConfigPaths() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(`[/Script/Pal.PalGameWorldSettings]
OptionSettings=(ServerName="Original")
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	panel.taskMu.Lock()
	panel.taskRunning = true
	panel.taskMu.Unlock()
	defer func() {
		panel.taskMu.Lock()
		panel.taskRunning = false
		panel.taskMu.Unlock()
	}()

	server, client := newAuthenticatedTestServer(t, panel)
	resp := doJSON(t, client, http.MethodPost, server.URL+"/api/config/backup", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("POST /api/config/backup status = %d, want %d", resp.StatusCode, http.StatusConflict)
	}
	assertNoConfigBakFiles(t, configPath)
	assertNoTasks(t, panel)
}

func TestConfigInitRejectsExternalServerBeforeInitialization(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	if _, err := panel.db.Exec(
		`INSERT INTO app_settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		"pal_server_path",
		serverPath,
	); err != nil {
		t.Fatalf("set pal_server_path: %v", err)
	}
	configPath, _, _, err := palConfigPaths(serverPath)
	if err != nil {
		t.Fatalf("palConfigPaths() error = %v", err)
	}
	panel.serverProcessDetector = func(settings settingsPayload) (bool, error) {
		return true, nil
	}

	server := httptest.NewServer(panel.Routes())
	defer server.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() error = %v", err)
	}
	client := &http.Client{Jar: jar}

	setup := map[string]string{"username": "admin", "password": "password123"}
	resp := doJSON(t, client, http.MethodPost, server.URL+"/api/auth/setup", setup)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = doJSON(t, client, http.MethodPost, server.URL+"/api/config/init", nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("POST /api/config/init status = %d, want %d", resp.StatusCode, http.StatusConflict)
	}
	resp.Body.Close()
	if fileExists(configPath) {
		t.Fatalf("config was initialized while external server was running: %s", configPath)
	}
}

func TestConfigBackupRejectsExternalServerWithoutBak(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	serverPath := t.TempDir()
	setTestAppSetting(t, panel, "pal_server_path", serverPath)
	configPath, _, _, err := palConfigPaths(serverPath)
	if err != nil {
		t.Fatalf("palConfigPaths() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(`[/Script/Pal.PalGameWorldSettings]
OptionSettings=(ServerName="Original")
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	detectorCalled := false
	panel.serverProcessDetector = func(settings settingsPayload) (bool, error) {
		detectorCalled = true
		return true, nil
	}

	server, client := newAuthenticatedTestServer(t, panel)
	resp := doJSON(t, client, http.MethodPost, server.URL+"/api/config/backup", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("POST /api/config/backup status = %d, want %d", resp.StatusCode, http.StatusConflict)
	}
	if !detectorCalled {
		t.Fatal("serverProcessDetector was not called")
	}
	assertNoConfigBakFiles(t, configPath)
	assertNoTasks(t, panel)
}

func assertNoConfigBakFiles(t *testing.T, configPath string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(configPath))
	if err != nil {
		t.Fatalf("read config dir: %v", err)
	}
	backupPrefix := filepath.Base(configPath) + "."
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, backupPrefix) && strings.HasSuffix(name, ".bak") {
			t.Fatalf("unexpected config backup file created: %s", name)
		}
	}
}

func setPalConfigFileLimit(t *testing.T, limit int64) {
	t.Helper()
	previous := maxPalConfigFileBytes
	maxPalConfigFileBytes = limit
	t.Cleanup(func() {
		maxPalConfigFileBytes = previous
	})
}
