package app

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const defaultPalWorldSettings = `[/Script/Pal.PalGameWorldSettings]
OptionSettings=(Difficulty=None,DayTimeSpeedRate=1.000000,NightTimeSpeedRate=1.000000,ExpRate=1.000000,PalCaptureRate=1.000000,PalSpawnNumRate=1.000000,EnemyDropItemRate=1.000000,CollectionDropRate=1.000000,CollectionObjectHpRate=1.000000,CollectionObjectRespawnSpeedRate=1.000000,EggDefaultHatchingTime=72.000000,BaseCampMaxNumInGuild=4,MaxBuildingLimitNum=0,ServerReplicatePawnCullDistance=15000.000000,ServerPlayerMaxNum=32,ServerName="Palworld Server",ServerDescription="",AdminPassword="",ServerPassword="",PublicPort=8211,PublicIP="",RCONEnabled=False,RCONPort=25575,RESTAPIEnabled=False,RESTAPIPort=8212,LogFormatType=Text,bAllowClientMod=False,bIsPvP=False,bEnablePlayerToPlayerDamage=False,bEnableDefenseOtherGuildPlayer=False,bIsUseBackupSaveData=True)
`

var errConfigNotInitialized = errors.New("PalWorldSettings.ini is not initialized; call POST /api/config/init")
var errPalConfigFileTooLarge = errors.New("palworld config file is too large")
var maxPalConfigFileBytes int64 = 4 << 20

type palConfigPayload struct {
	Exists       bool                `json:"exists"`
	ConfigPath   string              `json:"config_path"`
	DefaultPath  string              `json:"default_path"`
	Platform     string              `json:"platform"`
	BackupPath   string              `json:"backup_path,omitempty"`
	NeedsRestart bool                `json:"needs_restart,omitempty"`
	Values       palConfigValues     `json:"values"`
	RawValues    map[string]string   `json:"raw_values"`
	Fields       []palConfigFieldDef `json:"fields"`
}

type palConfigValues struct {
	ServerName                         string  `json:"server_name"`
	ServerDescription                  string  `json:"server_description"`
	AdminPassword                      string  `json:"admin_password"`
	ServerPassword                     string  `json:"server_password"`
	ServerPlayerMaxNum                 int     `json:"server_player_max_num"`
	Difficulty                         string  `json:"difficulty"`
	DayTimeSpeedRate                   float64 `json:"day_time_speed_rate"`
	NightTimeSpeedRate                 float64 `json:"night_time_speed_rate"`
	ExpRate                            float64 `json:"exp_rate"`
	PalCaptureRate                     float64 `json:"pal_capture_rate"`
	PalSpawnNumRate                    float64 `json:"pal_spawn_num_rate"`
	EnemyDropItemRate                  float64 `json:"enemy_drop_item_rate"`
	CollectionDropRate                 float64 `json:"collection_drop_rate"`
	CollectionObjectHpRate             float64 `json:"collection_object_hp_rate"`
	CollectionObjectRespawnSpeedRate   float64 `json:"collection_object_respawn_speed_rate"`
	EggDefaultHatchingTime             float64 `json:"egg_default_hatching_time"`
	DeathPenalty                       string  `json:"death_penalty"`
	BaseCampMaxNum                     int     `json:"base_camp_max_num"`
	BaseCampMaxNumInGuild              int     `json:"base_camp_max_num_in_guild"`
	BaseCampWorkerMaxNum               int     `json:"base_camp_worker_max_num"`
	GuildPlayerMaxNum                  int     `json:"guild_player_max_num"`
	BuildObjectDeteriorationDamageRate float64 `json:"build_object_deterioration_damage_rate"`
	MaxBuildingLimitNum                int     `json:"max_building_limit_num"`
	ServerReplicatePawnCullDistance    float64 `json:"server_replicate_pawn_cull_distance"`
	PublicPort                         int     `json:"public_port"`
	PublicIP                           string  `json:"public_ip"`
	RCONEnabled                        bool    `json:"rcon_enabled"`
	RCONPort                           int     `json:"rcon_port"`
	RESTAPIEnabled                     bool    `json:"rest_api_enabled"`
	RESTAPIPort                        int     `json:"rest_api_port"`
	LogFormatType                      string  `json:"log_format_type"`
	CrossplayPlatforms                 string  `json:"crossplay_platforms"`
	AllowClientMod                     bool    `json:"allow_client_mod"`
	IsPvP                              bool    `json:"is_pvp"`
	EnablePlayerToPlayerDamage         bool    `json:"enable_player_to_player_damage"`
	EnableDefenseOtherGuildPlayer      bool    `json:"enable_defense_other_guild_player"`
	IsUseBackupSaveData                bool    `json:"is_use_backup_save_data"`
}

type palConfigFieldDef struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Group string `json:"group"`
	Type  string `json:"type"`
}

type palConfigDocument struct {
	Content     string
	Start       int
	End         int
	Order       []string
	RawValues   map[string]string
	Values      palConfigValues
	ConfigPath  string
	DefaultPath string
	Platform    string
}

func (a *App) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	settings, err := a.loadSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	doc, err := a.loadPalConfigWithSettings(settings, false)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, errConfigNotInitialized)
			return
		}
		writeError(w, actionErrorStatus(err), err)
		return
	}
	writeJSON(w, http.StatusOK, doc.payload("", false))
}

func (a *App) handleInitConfig(w http.ResponseWriter, r *http.Request) {
	if !requireNoRequestBody(w, r) {
		return
	}
	settings, err := a.loadSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	needsInit, err := palConfigNeedsInitialization(settings)
	if err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	if needsInit {
		if err := a.ensureNoExternalServerRunning(settings); err != nil {
			writeError(w, actionErrorStatus(err), err)
			return
		}
		releaseTask, err := a.reserveTaskSlot()
		if err != nil {
			writeError(w, actionErrorStatus(err), err)
			return
		}
		defer releaseTask()
	}
	doc, err := a.loadPalConfigWithSettings(settings, true)
	if err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	writeJSON(w, http.StatusOK, doc.payload("", false))
}

func (a *App) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Values palConfigValues `json:"values"`
	}
	if !decodeJSON(w, r, &payload) {
		return
	}
	settings, err := a.loadSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := a.ensureNoExternalServerRunning(settings); err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	releaseTask, err := a.reserveTaskSlot()
	if err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	defer releaseTask()
	doc, err := a.loadPalConfigWithSettings(settings, true)
	if err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	payload.Values.applyDefaultsFrom(doc.Values)
	raw := doc.rawWithValues(payload.Values)
	nextContent := doc.render(raw)
	if err := checkPalConfigFileSize(doc.ConfigPath, int64(len(nextContent))); err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	if _, err := a.createBackupWithSettings(settings, "pre_config", "Before saving PalWorldSettings.ini"); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	backupPath, err := backupFile(doc.ConfigPath)
	if err != nil {
		writeError(w, configFileErrorStatus(err, http.StatusInternalServerError), err)
		return
	}
	if err := atomicWriteFile(doc.ConfigPath, []byte(nextContent), 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	nextDoc, err := readPalConfigDocument(doc.ConfigPath, doc.DefaultPath, doc.Platform)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, nextDoc.payload(backupPath, true))
}

func (a *App) handleConfigBackup(w http.ResponseWriter, r *http.Request) {
	if !requireNoRequestBody(w, r) {
		return
	}
	settings, err := a.loadSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := a.ensureNoExternalServerRunning(settings); err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	releaseTask, err := a.reserveTaskSlot()
	if err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	defer releaseTask()
	doc, err := a.loadPalConfigWithSettings(settings, true)
	if err != nil {
		writeError(w, actionErrorStatus(err), err)
		return
	}
	backupPath, err := backupFile(doc.ConfigPath)
	if err != nil {
		writeError(w, configFileErrorStatus(err, http.StatusInternalServerError), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"backup_path": backupPath})
}

func palConfigNeedsInitialization(settings settingsPayload) (bool, error) {
	serverPath := strings.TrimSpace(settings.PalServerPath)
	if serverPath == "" {
		return false, errors.New("pal_server_path is required before editing PalWorldSettings.ini")
	}
	configPath, _, _, err := palConfigPaths(serverPath)
	if err != nil {
		return false, err
	}
	return !fileExists(configPath), nil
}

func (a *App) loadPalConfig(create bool) (palConfigDocument, error) {
	settings, err := a.loadSettings()
	if err != nil {
		return palConfigDocument{}, err
	}
	return a.loadPalConfigWithSettings(settings, create)
}

func (a *App) loadPalConfigWithSettings(settings settingsPayload, create bool) (palConfigDocument, error) {
	serverPath := strings.TrimSpace(settings.PalServerPath)
	if serverPath == "" {
		return palConfigDocument{}, errors.New("pal_server_path is required before editing PalWorldSettings.ini")
	}
	configPath, defaultPath, platform, err := palConfigPaths(serverPath)
	if err != nil {
		return palConfigDocument{}, err
	}
	if create {
		if !fileExists(configPath) {
			if err := a.ensureNoExternalServerRunning(settings); err != nil {
				return palConfigDocument{}, err
			}
		}
		if err := ensurePalConfig(configPath, defaultPath); err != nil {
			return palConfigDocument{}, err
		}
	}
	return readPalConfigDocument(configPath, defaultPath, platform)
}

func palConfigPaths(serverPath string) (configPath string, defaultPath string, platform string, err error) {
	base, err := filepath.Abs(serverPath)
	if err != nil {
		return "", "", "", err
	}
	platform = "LinuxServer"
	if runtime.GOOS == "windows" {
		platform = "WindowsServer"
	}
	configPath = filepath.Join(base, "Pal", "Saved", "Config", platform, "PalWorldSettings.ini")
	defaultPath = filepath.Join(base, "DefaultPalWorldSettings.ini")
	if err := ensureWithin(base, configPath); err != nil {
		return "", "", "", err
	}
	if err := ensureWithin(base, defaultPath); err != nil {
		return "", "", "", err
	}
	return configPath, defaultPath, platform, nil
}

func ensureWithin(base, target string) error {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return err
	}
	if rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." && !filepath.IsAbs(rel)) {
		return nil
	}
	return fmt.Errorf("path escapes server directory: %s", target)
}

func ensurePalConfig(configPath, defaultPath string) error {
	if fileExists(configPath) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}
	if fileExists(defaultPath) {
		return copyPalConfigFile(defaultPath, configPath)
	}
	if err := checkPalConfigFileSize(configPath, int64(len(defaultPalWorldSettings))); err != nil {
		return err
	}
	return atomicWriteFile(configPath, []byte(defaultPalWorldSettings), 0o644)
}

func readPalConfigDocument(configPath, defaultPath, platform string) (palConfigDocument, error) {
	contentBytes, err := readPalConfigFile(configPath)
	if err != nil {
		return palConfigDocument{}, err
	}
	content := string(contentBytes)
	start, end, optionText, err := findOptionSettings(content)
	if err != nil {
		return palConfigDocument{}, err
	}
	order, raw, err := parseOptionSettings(optionText)
	if err != nil {
		return palConfigDocument{}, err
	}
	return palConfigDocument{
		Content:     content,
		Start:       start,
		End:         end,
		Order:       order,
		RawValues:   raw,
		Values:      valuesFromRaw(raw),
		ConfigPath:  configPath,
		DefaultPath: defaultPath,
		Platform:    platform,
	}, nil
}

func findOptionSettings(content string) (start int, end int, optionText string, err error) {
	marker := "OptionSettings=("
	start = strings.Index(content, marker)
	if start < 0 {
		return 0, 0, "", errors.New("OptionSettings=(...) was not found in PalWorldSettings.ini")
	}
	valueStart := start + len(marker)
	quote := rune(0)
	escaped := false
	depth := 1

	for offset, r := range content[valueStart:] {
		pos := valueStart + offset
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == quote {
				quote = 0
			}
			continue
		}

		switch r {
		case '"', '\'':
			quote = r
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return start, pos + 1, content[valueStart:pos], nil
			}
		}
	}
	return 0, 0, "", errors.New("OptionSettings parentheses are not balanced")
}

func parseOptionSettings(input string) ([]string, map[string]string, error) {
	parts := splitOptionParts(input)
	order := make([]string, 0, len(parts))
	values := make(map[string]string, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return nil, nil, fmt.Errorf("invalid OptionSettings item: %s", part)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			return nil, nil, errors.New("invalid OptionSettings item with empty key")
		}
		if _, exists := values[key]; !exists {
			order = append(order, key)
		}
		values[key] = value
	}
	return order, values, nil
}

func splitOptionParts(input string) []string {
	var parts []string
	var current strings.Builder
	quote := rune(0)
	escaped := false
	depth := 0
	for _, r := range input {
		if quote != 0 {
			current.WriteRune(r)
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == quote {
				quote = 0
			}
			continue
		}

		switch r {
		case '"', '\'':
			quote = r
			current.WriteRune(r)
		case '(', '[', '{':
			depth++
			current.WriteRune(r)
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
			current.WriteRune(r)
		case ',':
			if depth == 0 {
				parts = append(parts, current.String())
				current.Reset()
			} else {
				current.WriteRune(r)
			}
		default:
			current.WriteRune(r)
		}
	}
	parts = append(parts, current.String())
	return parts
}

func valuesFromRaw(raw map[string]string) palConfigValues {
	values := palConfigValues{
		ServerName:                         readString(raw, "ServerName", "Palworld Server"),
		ServerDescription:                  readString(raw, "ServerDescription", ""),
		AdminPassword:                      readString(raw, "AdminPassword", ""),
		ServerPassword:                     readString(raw, "ServerPassword", ""),
		ServerPlayerMaxNum:                 readInt(raw, "ServerPlayerMaxNum", 32),
		Difficulty:                         readString(raw, "Difficulty", "None"),
		DayTimeSpeedRate:                   readFloat(raw, "DayTimeSpeedRate", 1),
		NightTimeSpeedRate:                 readFloat(raw, "NightTimeSpeedRate", 1),
		ExpRate:                            readFloat(raw, "ExpRate", 1),
		PalCaptureRate:                     readFloat(raw, "PalCaptureRate", 1),
		PalSpawnNumRate:                    readFloat(raw, "PalSpawnNumRate", 1),
		EnemyDropItemRate:                  readFloat(raw, "EnemyDropItemRate", 1),
		CollectionDropRate:                 readFloat(raw, "CollectionDropRate", 1),
		CollectionObjectHpRate:             readFloat(raw, "CollectionObjectHpRate", 1),
		CollectionObjectRespawnSpeedRate:   readFloat(raw, "CollectionObjectRespawnSpeedRate", 1),
		EggDefaultHatchingTime:             readFloat(raw, "EggDefaultHatchingTime", 72),
		DeathPenalty:                       readString(raw, "DeathPenalty", "All"),
		BaseCampMaxNum:                     readInt(raw, "BaseCampMaxNum", 128),
		BaseCampMaxNumInGuild:              readInt(raw, "BaseCampMaxNumInGuild", 4),
		BaseCampWorkerMaxNum:               readInt(raw, "BaseCampWorkerMaxNum", 15),
		GuildPlayerMaxNum:                  readInt(raw, "GuildPlayerMaxNum", 20),
		BuildObjectDeteriorationDamageRate: readFloat(raw, "BuildObjectDeteriorationDamageRate", 1),
		MaxBuildingLimitNum:                readInt(raw, "MaxBuildingLimitNum", 0),
		ServerReplicatePawnCullDistance:    readFloat(raw, "ServerReplicatePawnCullDistance", 15000),
		PublicPort:                         readInt(raw, "PublicPort", 8211),
		PublicIP:                           readString(raw, "PublicIP", ""),
		RCONEnabled:                        readBool(raw, "RCONEnabled", false),
		RCONPort:                           readInt(raw, "RCONPort", 25575),
		RESTAPIEnabled:                     readBool(raw, "RESTAPIEnabled", false),
		RESTAPIPort:                        readInt(raw, "RESTAPIPort", 8212),
		LogFormatType:                      readString(raw, "LogFormatType", "Text"),
		CrossplayPlatforms:                 readString(raw, "CrossplayPlatforms", ""),
		AllowClientMod:                     readBool(raw, "bAllowClientMod", false),
		IsPvP:                              readBool(raw, "bIsPvP", false),
		EnablePlayerToPlayerDamage:         readBool(raw, "bEnablePlayerToPlayerDamage", false),
		EnableDefenseOtherGuildPlayer:      readBool(raw, "bEnableDefenseOtherGuildPlayer", false),
		IsUseBackupSaveData:                readBool(raw, "bIsUseBackupSaveData", true),
	}
	return values
}

func (values *palConfigValues) applyDefaultsFrom(previous palConfigValues) {
	if values.ServerPlayerMaxNum <= 0 {
		values.ServerPlayerMaxNum = previous.ServerPlayerMaxNum
	}
	if values.DayTimeSpeedRate <= 0 {
		values.DayTimeSpeedRate = previous.DayTimeSpeedRate
	}
	if values.NightTimeSpeedRate <= 0 {
		values.NightTimeSpeedRate = previous.NightTimeSpeedRate
	}
	if values.ExpRate <= 0 {
		values.ExpRate = previous.ExpRate
	}
	if values.PalCaptureRate <= 0 {
		values.PalCaptureRate = previous.PalCaptureRate
	}
	if values.PalSpawnNumRate <= 0 {
		values.PalSpawnNumRate = previous.PalSpawnNumRate
	}
	if values.EnemyDropItemRate <= 0 {
		values.EnemyDropItemRate = previous.EnemyDropItemRate
	}
	if values.CollectionDropRate <= 0 {
		values.CollectionDropRate = previous.CollectionDropRate
	}
	if values.CollectionObjectHpRate <= 0 {
		values.CollectionObjectHpRate = previous.CollectionObjectHpRate
	}
	if values.CollectionObjectRespawnSpeedRate <= 0 {
		values.CollectionObjectRespawnSpeedRate = previous.CollectionObjectRespawnSpeedRate
	}
	if values.EggDefaultHatchingTime < 0 {
		values.EggDefaultHatchingTime = previous.EggDefaultHatchingTime
	}
	if values.BaseCampMaxNum <= 0 {
		values.BaseCampMaxNum = previous.BaseCampMaxNum
	}
	if values.BaseCampMaxNumInGuild <= 0 {
		values.BaseCampMaxNumInGuild = previous.BaseCampMaxNumInGuild
	}
	if values.BaseCampMaxNumInGuild <= 0 {
		values.BaseCampMaxNumInGuild = 4
	}
	if values.BaseCampMaxNumInGuild > 10 {
		values.BaseCampMaxNumInGuild = 10
	}
	if values.BaseCampWorkerMaxNum <= 0 {
		values.BaseCampWorkerMaxNum = previous.BaseCampWorkerMaxNum
	}
	if values.GuildPlayerMaxNum <= 0 {
		values.GuildPlayerMaxNum = previous.GuildPlayerMaxNum
	}
	if values.BuildObjectDeteriorationDamageRate < 0 {
		values.BuildObjectDeteriorationDamageRate = previous.BuildObjectDeteriorationDamageRate
	}
	if values.MaxBuildingLimitNum < 0 {
		values.MaxBuildingLimitNum = previous.MaxBuildingLimitNum
	}
	if values.MaxBuildingLimitNum < 0 {
		values.MaxBuildingLimitNum = 0
	}
	if values.ServerReplicatePawnCullDistance <= 0 {
		values.ServerReplicatePawnCullDistance = previous.ServerReplicatePawnCullDistance
	}
	if values.ServerReplicatePawnCullDistance <= 0 {
		values.ServerReplicatePawnCullDistance = 15000
	}
	if values.ServerReplicatePawnCullDistance < 5000 {
		values.ServerReplicatePawnCullDistance = 5000
	}
	if values.ServerReplicatePawnCullDistance > 15000 {
		values.ServerReplicatePawnCullDistance = 15000
	}
	if values.PublicPort <= 0 {
		values.PublicPort = previous.PublicPort
	}
	if values.RCONPort <= 0 {
		values.RCONPort = previous.RCONPort
	}
	if values.RESTAPIPort <= 0 {
		values.RESTAPIPort = previous.RESTAPIPort
	}
	if values.ServerName == "" {
		values.ServerName = previous.ServerName
	}
	if values.Difficulty == "" {
		values.Difficulty = previous.Difficulty
	}
	if values.DeathPenalty == "" {
		values.DeathPenalty = previous.DeathPenalty
	}
	if values.LogFormatType == "" {
		values.LogFormatType = previous.LogFormatType
	}
}

func (doc palConfigDocument) rawWithValues(values palConfigValues) map[string]string {
	raw := make(map[string]string, len(doc.RawValues)+len(knownConfigKeys()))
	for key, value := range doc.RawValues {
		raw[key] = value
	}
	raw["ServerName"] = writeString(values.ServerName)
	raw["ServerDescription"] = writeString(values.ServerDescription)
	raw["AdminPassword"] = writeString(values.AdminPassword)
	raw["ServerPassword"] = writeString(values.ServerPassword)
	raw["ServerPlayerMaxNum"] = writeInt(values.ServerPlayerMaxNum)
	raw["Difficulty"] = writeEnum(values.Difficulty)
	raw["DayTimeSpeedRate"] = writeFloat(values.DayTimeSpeedRate)
	raw["NightTimeSpeedRate"] = writeFloat(values.NightTimeSpeedRate)
	raw["ExpRate"] = writeFloat(values.ExpRate)
	raw["PalCaptureRate"] = writeFloat(values.PalCaptureRate)
	raw["PalSpawnNumRate"] = writeFloat(values.PalSpawnNumRate)
	raw["EnemyDropItemRate"] = writeFloat(values.EnemyDropItemRate)
	raw["CollectionDropRate"] = writeFloat(values.CollectionDropRate)
	raw["CollectionObjectHpRate"] = writeFloat(values.CollectionObjectHpRate)
	raw["CollectionObjectRespawnSpeedRate"] = writeFloat(values.CollectionObjectRespawnSpeedRate)
	raw["EggDefaultHatchingTime"] = writeFloat(values.EggDefaultHatchingTime)
	raw["DeathPenalty"] = writeEnum(values.DeathPenalty)
	raw["BaseCampMaxNum"] = writeInt(values.BaseCampMaxNum)
	raw["BaseCampMaxNumInGuild"] = writeInt(values.BaseCampMaxNumInGuild)
	raw["BaseCampWorkerMaxNum"] = writeInt(values.BaseCampWorkerMaxNum)
	raw["GuildPlayerMaxNum"] = writeInt(values.GuildPlayerMaxNum)
	raw["BuildObjectDeteriorationDamageRate"] = writeFloat(values.BuildObjectDeteriorationDamageRate)
	raw["MaxBuildingLimitNum"] = writeInt(values.MaxBuildingLimitNum)
	raw["ServerReplicatePawnCullDistance"] = writeFloat(values.ServerReplicatePawnCullDistance)
	raw["PublicPort"] = writeInt(values.PublicPort)
	raw["PublicIP"] = writeString(values.PublicIP)
	raw["RCONEnabled"] = writeBool(values.RCONEnabled)
	raw["RCONPort"] = writeInt(values.RCONPort)
	raw["RESTAPIEnabled"] = writeBool(values.RESTAPIEnabled)
	raw["RESTAPIPort"] = writeInt(values.RESTAPIPort)
	raw["LogFormatType"] = writeEnum(values.LogFormatType)
	raw["CrossplayPlatforms"] = writeRawOrString(values.CrossplayPlatforms)
	raw["bAllowClientMod"] = writeBool(values.AllowClientMod)
	raw["bIsPvP"] = writeBool(values.IsPvP)
	raw["bEnablePlayerToPlayerDamage"] = writeBool(values.EnablePlayerToPlayerDamage)
	raw["bEnableDefenseOtherGuildPlayer"] = writeBool(values.EnableDefenseOtherGuildPlayer)
	raw["bIsUseBackupSaveData"] = writeBool(values.IsUseBackupSaveData)
	return raw
}

func (doc palConfigDocument) render(raw map[string]string) string {
	order := append([]string(nil), doc.Order...)
	seen := make(map[string]bool, len(order))
	for _, key := range order {
		seen[key] = true
	}
	for _, key := range knownConfigKeys() {
		if !seen[key] {
			order = append(order, key)
			seen[key] = true
		}
	}
	var inner strings.Builder
	for i, key := range order {
		value, ok := raw[key]
		if !ok {
			continue
		}
		if i > 0 {
			inner.WriteByte(',')
		}
		inner.WriteString(key)
		inner.WriteByte('=')
		inner.WriteString(value)
	}
	return doc.Content[:doc.Start] + "OptionSettings=(" + inner.String() + ")" + doc.Content[doc.End:]
}

func (doc palConfigDocument) payload(backupPath string, needsRestart bool) palConfigPayload {
	return palConfigPayload{
		Exists:       true,
		ConfigPath:   doc.ConfigPath,
		DefaultPath:  doc.DefaultPath,
		Platform:     doc.Platform,
		BackupPath:   backupPath,
		NeedsRestart: needsRestart,
		Values:       doc.Values,
		RawValues:    copyRawValues(doc.RawValues),
		Fields:       configFieldDefs(),
	}
}

func copyRawValues(input map[string]string) map[string]string {
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func knownConfigKeys() []string {
	return []string{
		"Difficulty",
		"DayTimeSpeedRate",
		"NightTimeSpeedRate",
		"ExpRate",
		"PalCaptureRate",
		"PalSpawnNumRate",
		"EnemyDropItemRate",
		"CollectionDropRate",
		"CollectionObjectHpRate",
		"CollectionObjectRespawnSpeedRate",
		"EggDefaultHatchingTime",
		"DeathPenalty",
		"BaseCampMaxNum",
		"BaseCampMaxNumInGuild",
		"BaseCampWorkerMaxNum",
		"GuildPlayerMaxNum",
		"BuildObjectDeteriorationDamageRate",
		"MaxBuildingLimitNum",
		"ServerReplicatePawnCullDistance",
		"ServerPlayerMaxNum",
		"ServerName",
		"ServerDescription",
		"AdminPassword",
		"ServerPassword",
		"PublicPort",
		"PublicIP",
		"RCONEnabled",
		"RCONPort",
		"RESTAPIEnabled",
		"RESTAPIPort",
		"LogFormatType",
		"CrossplayPlatforms",
		"bAllowClientMod",
		"bIsPvP",
		"bEnablePlayerToPlayerDamage",
		"bEnableDefenseOtherGuildPlayer",
		"bIsUseBackupSaveData",
	}
}

func configFieldDefs() []palConfigFieldDef {
	return []palConfigFieldDef{
		{Key: "server_name", Label: "服务器名称", Group: "基础设置", Type: "string"},
		{Key: "server_description", Label: "服务器描述", Group: "基础设置", Type: "string"},
		{Key: "admin_password", Label: "管理员密码", Group: "基础设置", Type: "password"},
		{Key: "server_password", Label: "服务器密码", Group: "基础设置", Type: "password"},
		{Key: "server_player_max_num", Label: "最大玩家数", Group: "基础设置", Type: "int"},
		{Key: "exp_rate", Label: "经验倍率", Group: "游戏倍率", Type: "float"},
		{Key: "pal_capture_rate", Label: "捕获倍率", Group: "游戏倍率", Type: "float"},
		{Key: "pal_spawn_num_rate", Label: "帕鲁刷新倍率", Group: "游戏倍率", Type: "float"},
		{Key: "enemy_drop_item_rate", Label: "敌人掉落倍率", Group: "游戏倍率", Type: "float"},
		{Key: "collection_drop_rate", Label: "采集掉落倍率", Group: "游戏倍率", Type: "float"},
		{Key: "day_time_speed_rate", Label: "白天速度", Group: "世界设置", Type: "float"},
		{Key: "night_time_speed_rate", Label: "夜晚速度", Group: "世界设置", Type: "float"},
		{Key: "egg_default_hatching_time", Label: "孵蛋时间", Group: "世界设置", Type: "float"},
		{Key: "death_penalty", Label: "死亡惩罚", Group: "世界设置", Type: "string"},
		{Key: "base_camp_max_num", Label: "据点总数", Group: "世界设置", Type: "int"},
		{Key: "base_camp_max_num_in_guild", Label: "公会据点上限", Group: "世界设置", Type: "int"},
		{Key: "base_camp_worker_max_num", Label: "据点工作帕鲁数", Group: "世界设置", Type: "int"},
		{Key: "guild_player_max_num", Label: "公会人数", Group: "世界设置", Type: "int"},
		{Key: "max_building_limit_num", Label: "建筑数量上限", Group: "世界设置", Type: "int"},
		{Key: "server_replicate_pawn_cull_distance", Label: "帕鲁同步距离", Group: "世界设置", Type: "float"},
		{Key: "public_port", Label: "Public Port", Group: "网络设置", Type: "int"},
		{Key: "public_ip", Label: "Public IP", Group: "网络设置", Type: "string"},
		{Key: "rcon_enabled", Label: "RCON", Group: "高级设置", Type: "bool"},
		{Key: "rcon_port", Label: "RCON Port", Group: "高级设置", Type: "int"},
		{Key: "rest_api_enabled", Label: "REST API", Group: "高级设置", Type: "bool"},
		{Key: "rest_api_port", Label: "REST API Port", Group: "高级设置", Type: "int"},
		{Key: "log_format_type", Label: "日志格式", Group: "高级设置", Type: "string"},
		{Key: "crossplay_platforms", Label: "跨平台连接", Group: "高级设置", Type: "string"},
		{Key: "allow_client_mod", Label: "允许客户端 MOD", Group: "高级设置", Type: "bool"},
		{Key: "is_pvp", Label: "PvP", Group: "高级设置", Type: "bool"},
		{Key: "enable_player_to_player_damage", Label: "玩家互伤", Group: "高级设置", Type: "bool"},
		{Key: "enable_defense_other_guild_player", Label: "据点防御其他公会玩家", Group: "高级设置", Type: "bool"},
		{Key: "is_use_backup_save_data", Label: "官方自动备份", Group: "高级设置", Type: "bool"},
	}
}

func readString(raw map[string]string, key, fallback string) string {
	value, ok := raw[key]
	if !ok || value == "" {
		return fallback
	}
	return unquoteConfigString(value)
}

func readInt(raw map[string]string, key string, fallback int) int {
	value, ok := raw[key]
	if !ok {
		return fallback
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(unquoteConfigString(value)))
	if err != nil {
		return fallback
	}
	return parsed
}

func readFloat(raw map[string]string, key string, fallback float64) float64 {
	value, ok := raw[key]
	if !ok {
		return fallback
	}
	parsed, err := strconv.ParseFloat(strings.TrimSpace(unquoteConfigString(value)), 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func readBool(raw map[string]string, key string, fallback bool) bool {
	value, ok := raw[key]
	if !ok {
		return fallback
	}
	switch strings.ToLower(strings.TrimSpace(unquoteConfigString(value))) {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	default:
		return fallback
	}
}

func writeString(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + replacer.Replace(value) + `"`
}

func writeEnum(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "None"
	}
	if strings.ContainsAny(value, " ,\"") {
		return writeString(value)
	}
	return value
}

func writeRawOrString(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return writeString(value)
	}
	if (strings.HasPrefix(value, "(") && strings.HasSuffix(value, ")")) ||
		(strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]")) ||
		(strings.HasPrefix(value, "{") && strings.HasSuffix(value, "}")) {
		return value
	}
	return writeString(value)
}

func writeInt(value int) string {
	return strconv.Itoa(value)
}

func writeFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', 6, 64)
}

func writeBool(value bool) string {
	if value {
		return "True"
	}
	return "False"
}

func unquoteConfigString(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 2 {
		return value
	}
	if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
		unquoted, err := strconv.Unquote(value)
		if err == nil {
			return unquoted
		}
		return value[1 : len(value)-1]
	}
	return value
}

func backupFile(path string) (string, error) {
	if !fileExists(path) {
		return "", fmt.Errorf("file not found: %s", path)
	}
	backupPath := fmt.Sprintf("%s.%s.bak", path, time.Now().UTC().Format("20060102T150405Z"))
	if err := copyPalConfigFile(path, backupPath); err != nil {
		return "", err
	}
	return backupPath, nil
}

func readPalConfigFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if info, err := file.Stat(); err == nil {
		if err := checkPalConfigFileSize(path, info.Size()); err != nil {
			return nil, err
		}
	}
	data, err := io.ReadAll(io.LimitReader(file, maxPalConfigFileBytes+1))
	if err != nil {
		return nil, err
	}
	if err := checkPalConfigFileSize(path, int64(len(data))); err != nil {
		return nil, err
	}
	return data, nil
}

func copyPalConfigFile(src, dst string) error {
	data, err := readPalConfigFile(src)
	if err != nil {
		return err
	}
	perm := os.FileMode(0o644)
	if info, err := os.Stat(src); err == nil {
		perm = info.Mode().Perm()
	}
	return atomicWriteFile(dst, data, perm)
}

func checkPalConfigFileSize(path string, size int64) error {
	if size > maxPalConfigFileBytes {
		return fmt.Errorf("%w: %s exceeds %d bytes", errPalConfigFileTooLarge, path, maxPalConfigFileBytes)
	}
	return nil
}

func configFileErrorStatus(err error, fallback int) int {
	if errors.Is(err, errPalConfigFileTooLarge) {
		return http.StatusRequestEntityTooLarge
	}
	return fallback
}

func copyFile(src, dst string) error {
	file, err := os.Open(src)
	if err != nil {
		return err
	}
	defer file.Close()
	perm := os.FileMode(0o644)
	if info, err := file.Stat(); err == nil {
		perm = info.Mode().Perm()
	}
	return atomicWriteFileFromReader(dst, file, perm)
}
