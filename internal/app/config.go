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

const defaultPalWorldSettings = `; This configuration file is a sample of the default server settings.
; Changes to this file will NOT be reflected on the server.
; To change the server settings, modify Pal/Saved/Config/LinuxServer/PalWorldSettings.ini.
[/Script/Pal.PalGameWorldSettings]
OptionSettings=(Difficulty=None,RandomizerType=None,RandomizerSeed="",bIsRandomizerPalLevelRandom=False,DayTimeSpeedRate=1.000000,NightTimeSpeedRate=1.000000,ExpRate=1.000000,PalCaptureRate=1.000000,PalSpawnNumRate=1.000000,PalDamageRateAttack=1.000000,PalDamageRateDefense=1.000000,PlayerDamageRateAttack=1.000000,PlayerDamageRateDefense=1.000000,PlayerStomachDecreaceRate=1.000000,PlayerStaminaDecreaceRate=1.000000,PlayerAutoHPRegeneRate=1.000000,PlayerAutoHpRegeneRateInSleep=1.000000,PalStomachDecreaceRate=1.000000,PalStaminaDecreaceRate=1.000000,PalAutoHPRegeneRate=1.000000,PalAutoHpRegeneRateInSleep=1.000000,BuildObjectHpRate=1.000000,BuildObjectDamageRate=1.000000,BuildObjectDeteriorationDamageRate=1.000000,CollectionDropRate=1.000000,CollectionObjectHpRate=1.000000,CollectionObjectRespawnSpeedRate=1.000000,EnemyDropItemRate=1.000000,DeathPenalty=All,bEnablePlayerToPlayerDamage=False,bEnableFriendlyFire=False,bEnableInvaderEnemy=True,bActiveUNKO=False,bEnableAimAssistPad=True,bEnableAimAssistKeyboard=False,DropItemMaxNum=3000,DropItemMaxNum_UNKO=100,BaseCampMaxNum=128,BaseCampWorkerMaxNum=15,DropItemAliveMaxHours=1.000000,bAutoResetGuildNoOnlinePlayers=False,AutoResetGuildTimeNoOnlinePlayers=72.000000,GuildPlayerMaxNum=20,BaseCampMaxNumInGuild=4,PalEggDefaultHatchingTime=72.000000,WorkSpeedRate=1.000000,AutoSaveSpan=30.000000,bIsMultiplay=False,bIsPvP=False,bHardcore=False,bPalLost=False,bCharacterRecreateInHardcore=False,bCanPickupOtherGuildDeathPenaltyDrop=False,bEnableNonLoginPenalty=True,bEnableFastTravel=True,bEnableFastTravelOnlyBaseCamp=False,bIsStartLocationSelectByMap=True,bExistPlayerAfterLogout=False,bEnableDefenseOtherGuildPlayer=False,bInvisibleOtherGuildBaseCampAreaFX=False,bBuildAreaLimit=False,ItemWeightRate=1.000000,CoopPlayerMaxNum=4,ServerPlayerMaxNum=32,ServerName="Default Palworld Server",ServerDescription="",AdminPassword="",ServerPassword="",bAllowClientMod=True,PublicPort=8211,PublicIP="",RCONEnabled=False,RCONPort=25575,Region="",bUseAuth=True,BanListURL="https://b.palworldgame.com/api/banlist.txt",RESTAPIEnabled=False,RESTAPIPort=8212,bShowPlayerList=False,ChatPostLimitPerMinute=30,CrossplayPlatforms=(Steam,Xbox,PS5,Mac),bIsUseBackupSaveData=True,LogFormatType=Text,bIsShowJoinLeftMessage=True,SupplyDropSpan=180,EnablePredatorBossPal=True,MaxBuildingLimitNum=0,ServerReplicatePawnCullDistance=15000.000000,bAllowGlobalPalboxExport=True,bAllowGlobalPalboxImport=False,EquipmentDurabilityDamageRate=1.000000,ItemContainerForceMarkDirtyInterval=1.000000,ItemCorruptionMultiplier=1.000000,DenyTechnologyList=,GuildRejoinCooldownMinutes=0,BlockRespawnTime=5.000000,RespawnPenaltyDurationThreshold=0.000000,RespawnPenaltyTimeScale=2.000000,bDisplayPvPItemNumOnWorldMap_BaseCamp=False,bDisplayPvPItemNumOnWorldMap_Player=False,AdditionalDropItemWhenPlayerKillingInPvPMode="PlayerDropItem",AdditionalDropItemNumWhenPlayerKillingInPvPMode=1,bAdditionalDropItemWhenPlayerKillingInPvPMode=False,bAllowEnhanceStat_Health=True,bAllowEnhanceStat_Attack=True,bAllowEnhanceStat_Stamina=True,bAllowEnhanceStat_Weight=True,bAllowEnhanceStat_WorkSpeed=True)
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
	Difficulty                                      string  `json:"difficulty"`
	RandomizerType                                  string  `json:"randomizer_type"`
	RandomizerSeed                                  string  `json:"randomizer_seed"`
	IsRandomizerPalLevelRandom                      bool    `json:"is_randomizer_pal_level_random"`
	DayTimeSpeedRate                                float64 `json:"day_time_speed_rate"`
	NightTimeSpeedRate                              float64 `json:"night_time_speed_rate"`
	ExpRate                                         float64 `json:"exp_rate"`
	PalCaptureRate                                  float64 `json:"pal_capture_rate"`
	PalSpawnNumRate                                 float64 `json:"pal_spawn_num_rate"`
	PalDamageRateAttack                             float64 `json:"pal_damage_rate_attack"`
	PalDamageRateDefense                            float64 `json:"pal_damage_rate_defense"`
	PlayerDamageRateAttack                          float64 `json:"player_damage_rate_attack"`
	PlayerDamageRateDefense                         float64 `json:"player_damage_rate_defense"`
	PlayerStomachDecreaseRate                       float64 `json:"player_stomach_decrease_rate"`
	PlayerStaminaDecreaseRate                       float64 `json:"player_stamina_decrease_rate"`
	PlayerAutoHPRegenRate                           float64 `json:"player_auto_hp_regen_rate"`
	PlayerAutoHPRegenRateInSleep                    float64 `json:"player_auto_hp_regen_rate_in_sleep"`
	PalStomachDecreaseRate                          float64 `json:"pal_stomach_decrease_rate"`
	PalStaminaDecreaseRate                          float64 `json:"pal_stamina_decrease_rate"`
	PalAutoHPRegenRate                              float64 `json:"pal_auto_hp_regen_rate"`
	PalAutoHPRegenRateInSleep                       float64 `json:"pal_auto_hp_regen_rate_in_sleep"`
	BuildObjectHpRate                               float64 `json:"build_object_hp_rate"`
	BuildObjectDamageRate                           float64 `json:"build_object_damage_rate"`
	BuildObjectDeteriorationDamageRate              float64 `json:"build_object_deterioration_damage_rate"`
	CollectionDropRate                              float64 `json:"collection_drop_rate"`
	CollectionObjectHpRate                          float64 `json:"collection_object_hp_rate"`
	CollectionObjectRespawnSpeedRate                float64 `json:"collection_object_respawn_speed_rate"`
	EnemyDropItemRate                               float64 `json:"enemy_drop_item_rate"`
	DeathPenalty                                    string  `json:"death_penalty"`
	EnablePlayerToPlayerDamage                      bool    `json:"enable_player_to_player_damage"`
	EnableFriendlyFire                              bool    `json:"enable_friendly_fire"`
	EnableInvaderEnemy                              bool    `json:"enable_invader_enemy"`
	ActiveUNKO                                      bool    `json:"active_unko"`
	EnableAimAssistPad                              bool    `json:"enable_aim_assist_pad"`
	EnableAimAssistKeyboard                         bool    `json:"enable_aim_assist_keyboard"`
	DropItemMaxNum                                  int     `json:"drop_item_max_num"`
	DropItemMaxNumUNKO                              int     `json:"drop_item_max_num_unko"`
	BaseCampMaxNum                                  int     `json:"base_camp_max_num"`
	BaseCampWorkerMaxNum                            int     `json:"base_camp_worker_max_num"`
	DropItemAliveMaxHours                           float64 `json:"drop_item_alive_max_hours"`
	AutoResetGuildNoOnlinePlayers                   bool    `json:"auto_reset_guild_no_online_players"`
	AutoResetGuildTimeNoOnlinePlayers               float64 `json:"auto_reset_guild_time_no_online_players"`
	GuildPlayerMaxNum                               int     `json:"guild_player_max_num"`
	BaseCampMaxNumInGuild                           int     `json:"base_camp_max_num_in_guild"`
	EggDefaultHatchingTime                          float64 `json:"egg_default_hatching_time"`
	WorkSpeedRate                                   float64 `json:"work_speed_rate"`
	AutoSaveSpan                                    float64 `json:"auto_save_span"`
	IsMultiplay                                     bool    `json:"is_multiplay"`
	IsPvP                                           bool    `json:"is_pvp"`
	Hardcore                                        bool    `json:"hardcore"`
	PalLost                                         bool    `json:"pal_lost"`
	CharacterRecreateInHardcore                     bool    `json:"character_recreate_in_hardcore"`
	CanPickupOtherGuildDeathPenaltyDrop             bool    `json:"can_pickup_other_guild_death_penalty_drop"`
	EnableNonLoginPenalty                           bool    `json:"enable_non_login_penalty"`
	EnableFastTravel                                bool    `json:"enable_fast_travel"`
	EnableFastTravelOnlyBaseCamp                    bool    `json:"enable_fast_travel_only_base_camp"`
	IsStartLocationSelectByMap                      bool    `json:"is_start_location_select_by_map"`
	ExistPlayerAfterLogout                          bool    `json:"exist_player_after_logout"`
	EnableDefenseOtherGuildPlayer                   bool    `json:"enable_defense_other_guild_player"`
	InvisibleOtherGuildBaseCampAreaFX               bool    `json:"invisible_other_guild_base_camp_area_fx"`
	BuildAreaLimit                                  bool    `json:"build_area_limit"`
	ItemWeightRate                                  float64 `json:"item_weight_rate"`
	CoopPlayerMaxNum                                int     `json:"coop_player_max_num"`
	ServerPlayerMaxNum                              int     `json:"server_player_max_num"`
	ServerName                                      string  `json:"server_name"`
	ServerDescription                               string  `json:"server_description"`
	AdminPassword                                   string  `json:"admin_password"`
	ServerPassword                                  string  `json:"server_password"`
	AllowClientMod                                  bool    `json:"allow_client_mod"`
	PublicPort                                      int     `json:"public_port"`
	PublicIP                                        string  `json:"public_ip"`
	RCONEnabled                                     bool    `json:"rcon_enabled"`
	RCONPort                                        int     `json:"rcon_port"`
	Region                                          string  `json:"region"`
	UseAuth                                         bool    `json:"use_auth"`
	BanListURL                                      string  `json:"ban_list_url"`
	RESTAPIEnabled                                  bool    `json:"rest_api_enabled"`
	RESTAPIPort                                     int     `json:"rest_api_port"`
	ShowPlayerList                                  bool    `json:"show_player_list"`
	ChatPostLimitPerMinute                          int     `json:"chat_post_limit_per_minute"`
	CrossplayPlatforms                              string  `json:"crossplay_platforms"`
	IsUseBackupSaveData                             bool    `json:"is_use_backup_save_data"`
	LogFormatType                                   string  `json:"log_format_type"`
	IsShowJoinLeftMessage                           bool    `json:"is_show_join_left_message"`
	SupplyDropSpan                                  int     `json:"supply_drop_span"`
	EnablePredatorBossPal                           bool    `json:"enable_predator_boss_pal"`
	MaxBuildingLimitNum                             int     `json:"max_building_limit_num"`
	ServerReplicatePawnCullDistance                 float64 `json:"server_replicate_pawn_cull_distance"`
	AllowGlobalPalboxExport                         bool    `json:"allow_global_palbox_export"`
	AllowGlobalPalboxImport                         bool    `json:"allow_global_palbox_import"`
	EquipmentDurabilityDamageRate                   float64 `json:"equipment_durability_damage_rate"`
	ItemContainerForceMarkDirtyInterval             float64 `json:"item_container_force_mark_dirty_interval"`
	ItemCorruptionMultiplier                        float64 `json:"item_corruption_multiplier"`
	DenyTechnologyList                              string  `json:"deny_technology_list"`
	GuildRejoinCooldownMinutes                      int     `json:"guild_rejoin_cooldown_minutes"`
	BlockRespawnTime                                float64 `json:"block_respawn_time"`
	RespawnPenaltyDurationThreshold                 float64 `json:"respawn_penalty_duration_threshold"`
	RespawnPenaltyTimeScale                         float64 `json:"respawn_penalty_time_scale"`
	DisplayPvPItemNumOnWorldMapBaseCamp             bool    `json:"display_pvp_item_num_on_world_map_base_camp"`
	DisplayPvPItemNumOnWorldMapPlayer               bool    `json:"display_pvp_item_num_on_world_map_player"`
	AdditionalDropItemWhenPlayerKillingInPvPMode    string  `json:"additional_drop_item_when_player_killing_in_pvp_mode"`
	AdditionalDropItemNumWhenPlayerKillingInPvPMode int     `json:"additional_drop_item_num_when_player_killing_in_pvp_mode"`
	AdditionalDropItemWhenPlayerKillingInPvPEnabled bool    `json:"additional_drop_item_when_player_killing_in_pvp_enabled"`
	AllowEnhanceStatHealth                          bool    `json:"allow_enhance_stat_health"`
	AllowEnhanceStatAttack                          bool    `json:"allow_enhance_stat_attack"`
	AllowEnhanceStatStamina                         bool    `json:"allow_enhance_stat_stamina"`
	AllowEnhanceStatWeight                          bool    `json:"allow_enhance_stat_weight"`
	AllowEnhanceStatWorkSpeed                       bool    `json:"allow_enhance_stat_work_speed"`
}

type palConfigFieldOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type palConfigFieldDef struct {
	Key         string                 `json:"key"`
	RawKey      string                 `json:"raw_key"`
	Label       string                 `json:"label"`
	Group       string                 `json:"group"`
	Type        string                 `json:"type"`
	Description string                 `json:"description,omitempty"`
	Min         *float64               `json:"min,omitempty"`
	Max         *float64               `json:"max,omitempty"`
	Step        *float64               `json:"step,omitempty"`
	Options     []palConfigFieldOption `json:"options,omitempty"`
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
		Difficulty:                                   readString(raw, "Difficulty", "None"),
		RandomizerType:                               readString(raw, "RandomizerType", "None"),
		RandomizerSeed:                               readString(raw, "RandomizerSeed", ""),
		IsRandomizerPalLevelRandom:                   readBool(raw, "bIsRandomizerPalLevelRandom", false),
		DayTimeSpeedRate:                             readFloat(raw, "DayTimeSpeedRate", 1),
		NightTimeSpeedRate:                           readFloat(raw, "NightTimeSpeedRate", 1),
		ExpRate:                                      readFloat(raw, "ExpRate", 1),
		PalCaptureRate:                               readFloat(raw, "PalCaptureRate", 1),
		PalSpawnNumRate:                              readFloat(raw, "PalSpawnNumRate", 1),
		PalDamageRateAttack:                          readFloat(raw, "PalDamageRateAttack", 1),
		PalDamageRateDefense:                         readFloat(raw, "PalDamageRateDefense", 1),
		PlayerDamageRateAttack:                       readFloat(raw, "PlayerDamageRateAttack", 1),
		PlayerDamageRateDefense:                      readFloat(raw, "PlayerDamageRateDefense", 1),
		PlayerStomachDecreaseRate:                    readFloat(raw, "PlayerStomachDecreaceRate", 1),
		PlayerStaminaDecreaseRate:                    readFloat(raw, "PlayerStaminaDecreaceRate", 1),
		PlayerAutoHPRegenRate:                        readFloat(raw, "PlayerAutoHPRegeneRate", 1),
		PlayerAutoHPRegenRateInSleep:                 readFloat(raw, "PlayerAutoHpRegeneRateInSleep", 1),
		PalStomachDecreaseRate:                       readFloat(raw, "PalStomachDecreaceRate", 1),
		PalStaminaDecreaseRate:                       readFloat(raw, "PalStaminaDecreaceRate", 1),
		PalAutoHPRegenRate:                           readFloat(raw, "PalAutoHPRegeneRate", 1),
		PalAutoHPRegenRateInSleep:                    readFloat(raw, "PalAutoHpRegeneRateInSleep", 1),
		BuildObjectHpRate:                            readFloat(raw, "BuildObjectHpRate", 1),
		BuildObjectDamageRate:                        readFloat(raw, "BuildObjectDamageRate", 1),
		BuildObjectDeteriorationDamageRate:           readFloat(raw, "BuildObjectDeteriorationDamageRate", 1),
		CollectionDropRate:                           readFloat(raw, "CollectionDropRate", 1),
		CollectionObjectHpRate:                       readFloat(raw, "CollectionObjectHpRate", 1),
		CollectionObjectRespawnSpeedRate:             readFloat(raw, "CollectionObjectRespawnSpeedRate", 1),
		EnemyDropItemRate:                            readFloat(raw, "EnemyDropItemRate", 1),
		DeathPenalty:                                 readString(raw, "DeathPenalty", "All"),
		EnablePlayerToPlayerDamage:                   readBool(raw, "bEnablePlayerToPlayerDamage", false),
		EnableFriendlyFire:                           readBool(raw, "bEnableFriendlyFire", false),
		EnableInvaderEnemy:                           readBool(raw, "bEnableInvaderEnemy", true),
		ActiveUNKO:                                   readBool(raw, "bActiveUNKO", false),
		EnableAimAssistPad:                           readBool(raw, "bEnableAimAssistPad", true),
		EnableAimAssistKeyboard:                      readBool(raw, "bEnableAimAssistKeyboard", false),
		DropItemMaxNum:                               readInt(raw, "DropItemMaxNum", 3000),
		DropItemMaxNumUNKO:                           readInt(raw, "DropItemMaxNum_UNKO", 100),
		BaseCampMaxNum:                               readInt(raw, "BaseCampMaxNum", 128),
		BaseCampWorkerMaxNum:                         readInt(raw, "BaseCampWorkerMaxNum", 15),
		DropItemAliveMaxHours:                        readFloat(raw, "DropItemAliveMaxHours", 1),
		AutoResetGuildNoOnlinePlayers:                readBool(raw, "bAutoResetGuildNoOnlinePlayers", false),
		AutoResetGuildTimeNoOnlinePlayers:            readFloat(raw, "AutoResetGuildTimeNoOnlinePlayers", 72),
		GuildPlayerMaxNum:                            readInt(raw, "GuildPlayerMaxNum", 20),
		BaseCampMaxNumInGuild:                        readInt(raw, "BaseCampMaxNumInGuild", 4),
		EggDefaultHatchingTime:                       readFloatWithFallback(raw, "PalEggDefaultHatchingTime", "EggDefaultHatchingTime", 72),
		WorkSpeedRate:                                readFloat(raw, "WorkSpeedRate", 1),
		AutoSaveSpan:                                 readFloat(raw, "AutoSaveSpan", 30),
		IsMultiplay:                                  readBool(raw, "bIsMultiplay", false),
		IsPvP:                                        readBool(raw, "bIsPvP", false),
		Hardcore:                                     readBool(raw, "bHardcore", false),
		PalLost:                                      readBool(raw, "bPalLost", false),
		CharacterRecreateInHardcore:                  readBool(raw, "bCharacterRecreateInHardcore", false),
		CanPickupOtherGuildDeathPenaltyDrop:          readBool(raw, "bCanPickupOtherGuildDeathPenaltyDrop", false),
		EnableNonLoginPenalty:                        readBool(raw, "bEnableNonLoginPenalty", true),
		EnableFastTravel:                             readBool(raw, "bEnableFastTravel", true),
		EnableFastTravelOnlyBaseCamp:                 readBool(raw, "bEnableFastTravelOnlyBaseCamp", false),
		IsStartLocationSelectByMap:                   readBool(raw, "bIsStartLocationSelectByMap", true),
		ExistPlayerAfterLogout:                       readBool(raw, "bExistPlayerAfterLogout", false),
		EnableDefenseOtherGuildPlayer:                readBool(raw, "bEnableDefenseOtherGuildPlayer", false),
		InvisibleOtherGuildBaseCampAreaFX:            readBool(raw, "bInvisibleOtherGuildBaseCampAreaFX", false),
		BuildAreaLimit:                               readBool(raw, "bBuildAreaLimit", false),
		ItemWeightRate:                               readFloat(raw, "ItemWeightRate", 1),
		CoopPlayerMaxNum:                             readInt(raw, "CoopPlayerMaxNum", 4),
		ServerPlayerMaxNum:                           readInt(raw, "ServerPlayerMaxNum", 32),
		ServerName:                                   readString(raw, "ServerName", "Default Palworld Server"),
		ServerDescription:                            readString(raw, "ServerDescription", ""),
		AdminPassword:                                readString(raw, "AdminPassword", ""),
		ServerPassword:                               readString(raw, "ServerPassword", ""),
		AllowClientMod:                               readBool(raw, "bAllowClientMod", true),
		PublicPort:                                   readInt(raw, "PublicPort", 8211),
		PublicIP:                                     readString(raw, "PublicIP", ""),
		RCONEnabled:                                  readBool(raw, "RCONEnabled", false),
		RCONPort:                                     readInt(raw, "RCONPort", 25575),
		Region:                                       readString(raw, "Region", ""),
		UseAuth:                                      readBool(raw, "bUseAuth", true),
		BanListURL:                                   readString(raw, "BanListURL", "https://b.palworldgame.com/api/banlist.txt"),
		RESTAPIEnabled:                               readBool(raw, "RESTAPIEnabled", false),
		RESTAPIPort:                                  readInt(raw, "RESTAPIPort", 8212),
		ShowPlayerList:                               readBool(raw, "bShowPlayerList", false),
		ChatPostLimitPerMinute:                       readInt(raw, "ChatPostLimitPerMinute", 30),
		CrossplayPlatforms:                           readString(raw, "CrossplayPlatforms", "(Steam,Xbox,PS5,Mac)"),
		IsUseBackupSaveData:                          readBool(raw, "bIsUseBackupSaveData", true),
		LogFormatType:                                readString(raw, "LogFormatType", "Text"),
		IsShowJoinLeftMessage:                        readBool(raw, "bIsShowJoinLeftMessage", true),
		SupplyDropSpan:                               readInt(raw, "SupplyDropSpan", 180),
		EnablePredatorBossPal:                        readBool(raw, "EnablePredatorBossPal", true),
		MaxBuildingLimitNum:                          readInt(raw, "MaxBuildingLimitNum", 0),
		ServerReplicatePawnCullDistance:              readFloat(raw, "ServerReplicatePawnCullDistance", 15000),
		AllowGlobalPalboxExport:                      readBool(raw, "bAllowGlobalPalboxExport", true),
		AllowGlobalPalboxImport:                      readBool(raw, "bAllowGlobalPalboxImport", false),
		EquipmentDurabilityDamageRate:                readFloat(raw, "EquipmentDurabilityDamageRate", 1),
		ItemContainerForceMarkDirtyInterval:          readFloat(raw, "ItemContainerForceMarkDirtyInterval", 1),
		ItemCorruptionMultiplier:                     readFloat(raw, "ItemCorruptionMultiplier", 1),
		DenyTechnologyList:                           readString(raw, "DenyTechnologyList", ""),
		GuildRejoinCooldownMinutes:                   readInt(raw, "GuildRejoinCooldownMinutes", 0),
		BlockRespawnTime:                             readFloat(raw, "BlockRespawnTime", 5),
		RespawnPenaltyDurationThreshold:              readFloat(raw, "RespawnPenaltyDurationThreshold", 0),
		RespawnPenaltyTimeScale:                      readFloat(raw, "RespawnPenaltyTimeScale", 2),
		DisplayPvPItemNumOnWorldMapBaseCamp:          readBool(raw, "bDisplayPvPItemNumOnWorldMap_BaseCamp", false),
		DisplayPvPItemNumOnWorldMapPlayer:            readBool(raw, "bDisplayPvPItemNumOnWorldMap_Player", false),
		AdditionalDropItemWhenPlayerKillingInPvPMode: readString(raw, "AdditionalDropItemWhenPlayerKillingInPvPMode", "PlayerDropItem"),
		AdditionalDropItemNumWhenPlayerKillingInPvPMode: readInt(raw, "AdditionalDropItemNumWhenPlayerKillingInPvPMode", 1),
		AdditionalDropItemWhenPlayerKillingInPvPEnabled: readBool(raw, "bAdditionalDropItemWhenPlayerKillingInPvPMode", false),
		AllowEnhanceStatHealth:                          readBool(raw, "bAllowEnhanceStat_Health", true),
		AllowEnhanceStatAttack:                          readBool(raw, "bAllowEnhanceStat_Attack", true),
		AllowEnhanceStatStamina:                         readBool(raw, "bAllowEnhanceStat_Stamina", true),
		AllowEnhanceStatWeight:                          readBool(raw, "bAllowEnhanceStat_Weight", true),
		AllowEnhanceStatWorkSpeed:                       readBool(raw, "bAllowEnhanceStat_WorkSpeed", true),
	}
	return values
}

func (values *palConfigValues) applyDefaultsFrom(previous palConfigValues) {
	positiveFloat := func(value *float64, previousValue, fallback float64) {
		if *value <= 0 {
			*value = previousValue
		}
		if *value <= 0 {
			*value = fallback
		}
	}
	nonNegativeFloat := func(value *float64, previousValue, fallback float64) {
		if *value < 0 {
			*value = previousValue
		}
		if *value < 0 {
			*value = fallback
		}
	}
	positiveInt := func(value *int, previousValue, fallback int) {
		if *value <= 0 {
			*value = previousValue
		}
		if *value <= 0 {
			*value = fallback
		}
	}
	nonNegativeInt := func(value *int, previousValue, fallback int) {
		if *value < 0 {
			*value = previousValue
		}
		if *value < 0 {
			*value = fallback
		}
	}
	port := func(value *int, previousValue, fallback int) {
		positiveInt(value, previousValue, fallback)
		if *value > 65535 {
			*value = previousValue
		}
		if *value <= 0 || *value > 65535 {
			*value = fallback
		}
	}

	positiveFloat(&values.DayTimeSpeedRate, previous.DayTimeSpeedRate, 1)
	positiveFloat(&values.NightTimeSpeedRate, previous.NightTimeSpeedRate, 1)
	positiveFloat(&values.ExpRate, previous.ExpRate, 1)
	positiveFloat(&values.PalCaptureRate, previous.PalCaptureRate, 1)
	positiveFloat(&values.PalSpawnNumRate, previous.PalSpawnNumRate, 1)
	positiveFloat(&values.PalDamageRateAttack, previous.PalDamageRateAttack, 1)
	positiveFloat(&values.PalDamageRateDefense, previous.PalDamageRateDefense, 1)
	positiveFloat(&values.PlayerDamageRateAttack, previous.PlayerDamageRateAttack, 1)
	positiveFloat(&values.PlayerDamageRateDefense, previous.PlayerDamageRateDefense, 1)
	positiveFloat(&values.PlayerStomachDecreaseRate, previous.PlayerStomachDecreaseRate, 1)
	positiveFloat(&values.PlayerStaminaDecreaseRate, previous.PlayerStaminaDecreaseRate, 1)
	positiveFloat(&values.PlayerAutoHPRegenRate, previous.PlayerAutoHPRegenRate, 1)
	positiveFloat(&values.PlayerAutoHPRegenRateInSleep, previous.PlayerAutoHPRegenRateInSleep, 1)
	positiveFloat(&values.PalStomachDecreaseRate, previous.PalStomachDecreaseRate, 1)
	positiveFloat(&values.PalStaminaDecreaseRate, previous.PalStaminaDecreaseRate, 1)
	positiveFloat(&values.PalAutoHPRegenRate, previous.PalAutoHPRegenRate, 1)
	positiveFloat(&values.PalAutoHPRegenRateInSleep, previous.PalAutoHPRegenRateInSleep, 1)
	positiveFloat(&values.BuildObjectHpRate, previous.BuildObjectHpRate, 1)
	positiveFloat(&values.BuildObjectDamageRate, previous.BuildObjectDamageRate, 1)
	nonNegativeFloat(&values.BuildObjectDeteriorationDamageRate, previous.BuildObjectDeteriorationDamageRate, 1)
	positiveFloat(&values.CollectionDropRate, previous.CollectionDropRate, 1)
	positiveFloat(&values.CollectionObjectHpRate, previous.CollectionObjectHpRate, 1)
	positiveFloat(&values.CollectionObjectRespawnSpeedRate, previous.CollectionObjectRespawnSpeedRate, 1)
	positiveFloat(&values.EnemyDropItemRate, previous.EnemyDropItemRate, 1)
	positiveFloat(&values.DropItemAliveMaxHours, previous.DropItemAliveMaxHours, 1)
	nonNegativeFloat(&values.AutoResetGuildTimeNoOnlinePlayers, previous.AutoResetGuildTimeNoOnlinePlayers, 72)
	nonNegativeFloat(&values.EggDefaultHatchingTime, previous.EggDefaultHatchingTime, 72)
	positiveFloat(&values.WorkSpeedRate, previous.WorkSpeedRate, 1)
	positiveFloat(&values.AutoSaveSpan, previous.AutoSaveSpan, 30)
	positiveFloat(&values.ItemWeightRate, previous.ItemWeightRate, 1)
	positiveFloat(&values.EquipmentDurabilityDamageRate, previous.EquipmentDurabilityDamageRate, 1)
	positiveFloat(&values.ItemContainerForceMarkDirtyInterval, previous.ItemContainerForceMarkDirtyInterval, 1)
	nonNegativeFloat(&values.ItemCorruptionMultiplier, previous.ItemCorruptionMultiplier, 1)
	nonNegativeFloat(&values.BlockRespawnTime, previous.BlockRespawnTime, 5)
	nonNegativeFloat(&values.RespawnPenaltyDurationThreshold, previous.RespawnPenaltyDurationThreshold, 0)
	positiveFloat(&values.RespawnPenaltyTimeScale, previous.RespawnPenaltyTimeScale, 2)

	positiveInt(&values.DropItemMaxNum, previous.DropItemMaxNum, 3000)
	positiveInt(&values.DropItemMaxNumUNKO, previous.DropItemMaxNumUNKO, 100)
	positiveInt(&values.BaseCampMaxNum, previous.BaseCampMaxNum, 128)
	positiveInt(&values.BaseCampWorkerMaxNum, previous.BaseCampWorkerMaxNum, 15)
	if values.BaseCampWorkerMaxNum > 50 {
		values.BaseCampWorkerMaxNum = 50
	}
	positiveInt(&values.GuildPlayerMaxNum, previous.GuildPlayerMaxNum, 20)
	positiveInt(&values.BaseCampMaxNumInGuild, previous.BaseCampMaxNumInGuild, 4)
	if values.BaseCampMaxNumInGuild > 10 {
		values.BaseCampMaxNumInGuild = 10
	}
	positiveInt(&values.CoopPlayerMaxNum, previous.CoopPlayerMaxNum, 4)
	positiveInt(&values.ServerPlayerMaxNum, previous.ServerPlayerMaxNum, 32)
	nonNegativeInt(&values.ChatPostLimitPerMinute, previous.ChatPostLimitPerMinute, 30)
	nonNegativeInt(&values.SupplyDropSpan, previous.SupplyDropSpan, 180)
	nonNegativeInt(&values.MaxBuildingLimitNum, previous.MaxBuildingLimitNum, 0)
	nonNegativeInt(&values.GuildRejoinCooldownMinutes, previous.GuildRejoinCooldownMinutes, 0)
	nonNegativeInt(&values.AdditionalDropItemNumWhenPlayerKillingInPvPMode, previous.AdditionalDropItemNumWhenPlayerKillingInPvPMode, 1)
	port(&values.PublicPort, previous.PublicPort, 8211)
	port(&values.RCONPort, previous.RCONPort, 25575)
	port(&values.RESTAPIPort, previous.RESTAPIPort, 8212)

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

	if values.ServerName == "" {
		values.ServerName = previous.ServerName
	}
	if values.Difficulty == "" {
		values.Difficulty = previous.Difficulty
	}
	if values.RandomizerType == "" {
		values.RandomizerType = previous.RandomizerType
	}
	if values.DeathPenalty == "" {
		values.DeathPenalty = previous.DeathPenalty
	}
	if values.LogFormatType == "" {
		values.LogFormatType = previous.LogFormatType
	}
	if values.BanListURL == "" {
		values.BanListURL = previous.BanListURL
	}
	if values.AdditionalDropItemWhenPlayerKillingInPvPMode == "" {
		values.AdditionalDropItemWhenPlayerKillingInPvPMode = previous.AdditionalDropItemWhenPlayerKillingInPvPMode
	}
}

func (doc palConfigDocument) rawWithValues(values palConfigValues) map[string]string {
	raw := make(map[string]string, len(doc.RawValues)+len(knownConfigKeys()))
	for key, value := range doc.RawValues {
		raw[key] = value
	}
	raw["Difficulty"] = writeEnum(values.Difficulty)
	raw["RandomizerType"] = writeEnum(values.RandomizerType)
	raw["RandomizerSeed"] = writeString(values.RandomizerSeed)
	raw["bIsRandomizerPalLevelRandom"] = writeBool(values.IsRandomizerPalLevelRandom)
	raw["DayTimeSpeedRate"] = writeFloat(values.DayTimeSpeedRate)
	raw["NightTimeSpeedRate"] = writeFloat(values.NightTimeSpeedRate)
	raw["ExpRate"] = writeFloat(values.ExpRate)
	raw["PalCaptureRate"] = writeFloat(values.PalCaptureRate)
	raw["PalSpawnNumRate"] = writeFloat(values.PalSpawnNumRate)
	raw["PalDamageRateAttack"] = writeFloat(values.PalDamageRateAttack)
	raw["PalDamageRateDefense"] = writeFloat(values.PalDamageRateDefense)
	raw["PlayerDamageRateAttack"] = writeFloat(values.PlayerDamageRateAttack)
	raw["PlayerDamageRateDefense"] = writeFloat(values.PlayerDamageRateDefense)
	raw["PlayerStomachDecreaceRate"] = writeFloat(values.PlayerStomachDecreaseRate)
	raw["PlayerStaminaDecreaceRate"] = writeFloat(values.PlayerStaminaDecreaseRate)
	raw["PlayerAutoHPRegeneRate"] = writeFloat(values.PlayerAutoHPRegenRate)
	raw["PlayerAutoHpRegeneRateInSleep"] = writeFloat(values.PlayerAutoHPRegenRateInSleep)
	raw["PalStomachDecreaceRate"] = writeFloat(values.PalStomachDecreaseRate)
	raw["PalStaminaDecreaceRate"] = writeFloat(values.PalStaminaDecreaseRate)
	raw["PalAutoHPRegeneRate"] = writeFloat(values.PalAutoHPRegenRate)
	raw["PalAutoHpRegeneRateInSleep"] = writeFloat(values.PalAutoHPRegenRateInSleep)
	raw["BuildObjectHpRate"] = writeFloat(values.BuildObjectHpRate)
	raw["BuildObjectDamageRate"] = writeFloat(values.BuildObjectDamageRate)
	raw["BuildObjectDeteriorationDamageRate"] = writeFloat(values.BuildObjectDeteriorationDamageRate)
	raw["EnemyDropItemRate"] = writeFloat(values.EnemyDropItemRate)
	raw["CollectionDropRate"] = writeFloat(values.CollectionDropRate)
	raw["CollectionObjectHpRate"] = writeFloat(values.CollectionObjectHpRate)
	raw["CollectionObjectRespawnSpeedRate"] = writeFloat(values.CollectionObjectRespawnSpeedRate)
	raw["DeathPenalty"] = writeEnum(values.DeathPenalty)
	raw["bEnablePlayerToPlayerDamage"] = writeBool(values.EnablePlayerToPlayerDamage)
	raw["bEnableFriendlyFire"] = writeBool(values.EnableFriendlyFire)
	raw["bEnableInvaderEnemy"] = writeBool(values.EnableInvaderEnemy)
	raw["bActiveUNKO"] = writeBool(values.ActiveUNKO)
	raw["bEnableAimAssistPad"] = writeBool(values.EnableAimAssistPad)
	raw["bEnableAimAssistKeyboard"] = writeBool(values.EnableAimAssistKeyboard)
	raw["DropItemMaxNum"] = writeInt(values.DropItemMaxNum)
	raw["DropItemMaxNum_UNKO"] = writeInt(values.DropItemMaxNumUNKO)
	raw["BaseCampMaxNum"] = writeInt(values.BaseCampMaxNum)
	raw["BaseCampWorkerMaxNum"] = writeInt(values.BaseCampWorkerMaxNum)
	raw["DropItemAliveMaxHours"] = writeFloat(values.DropItemAliveMaxHours)
	raw["bAutoResetGuildNoOnlinePlayers"] = writeBool(values.AutoResetGuildNoOnlinePlayers)
	raw["AutoResetGuildTimeNoOnlinePlayers"] = writeFloat(values.AutoResetGuildTimeNoOnlinePlayers)
	raw["GuildPlayerMaxNum"] = writeInt(values.GuildPlayerMaxNum)
	raw["BaseCampMaxNumInGuild"] = writeInt(values.BaseCampMaxNumInGuild)
	raw["PalEggDefaultHatchingTime"] = writeFloat(values.EggDefaultHatchingTime)
	delete(raw, "EggDefaultHatchingTime")
	raw["WorkSpeedRate"] = writeFloat(values.WorkSpeedRate)
	raw["AutoSaveSpan"] = writeFloat(values.AutoSaveSpan)
	raw["bIsMultiplay"] = writeBool(values.IsMultiplay)
	raw["bIsPvP"] = writeBool(values.IsPvP)
	raw["bHardcore"] = writeBool(values.Hardcore)
	raw["bPalLost"] = writeBool(values.PalLost)
	raw["bCharacterRecreateInHardcore"] = writeBool(values.CharacterRecreateInHardcore)
	raw["bCanPickupOtherGuildDeathPenaltyDrop"] = writeBool(values.CanPickupOtherGuildDeathPenaltyDrop)
	raw["bEnableNonLoginPenalty"] = writeBool(values.EnableNonLoginPenalty)
	raw["bEnableFastTravel"] = writeBool(values.EnableFastTravel)
	raw["bEnableFastTravelOnlyBaseCamp"] = writeBool(values.EnableFastTravelOnlyBaseCamp)
	raw["bIsStartLocationSelectByMap"] = writeBool(values.IsStartLocationSelectByMap)
	raw["bExistPlayerAfterLogout"] = writeBool(values.ExistPlayerAfterLogout)
	raw["bEnableDefenseOtherGuildPlayer"] = writeBool(values.EnableDefenseOtherGuildPlayer)
	raw["bInvisibleOtherGuildBaseCampAreaFX"] = writeBool(values.InvisibleOtherGuildBaseCampAreaFX)
	raw["bBuildAreaLimit"] = writeBool(values.BuildAreaLimit)
	raw["ItemWeightRate"] = writeFloat(values.ItemWeightRate)
	raw["CoopPlayerMaxNum"] = writeInt(values.CoopPlayerMaxNum)
	raw["ServerPlayerMaxNum"] = writeInt(values.ServerPlayerMaxNum)
	raw["ServerName"] = writeString(values.ServerName)
	raw["ServerDescription"] = writeString(values.ServerDescription)
	raw["AdminPassword"] = writeString(values.AdminPassword)
	raw["ServerPassword"] = writeString(values.ServerPassword)
	raw["bAllowClientMod"] = writeBool(values.AllowClientMod)
	raw["PublicPort"] = writeInt(values.PublicPort)
	raw["PublicIP"] = writeString(values.PublicIP)
	raw["RCONEnabled"] = writeBool(values.RCONEnabled)
	raw["RCONPort"] = writeInt(values.RCONPort)
	raw["Region"] = writeString(values.Region)
	raw["bUseAuth"] = writeBool(values.UseAuth)
	raw["BanListURL"] = writeString(values.BanListURL)
	raw["RESTAPIEnabled"] = writeBool(values.RESTAPIEnabled)
	raw["RESTAPIPort"] = writeInt(values.RESTAPIPort)
	raw["bShowPlayerList"] = writeBool(values.ShowPlayerList)
	raw["ChatPostLimitPerMinute"] = writeInt(values.ChatPostLimitPerMinute)
	raw["LogFormatType"] = writeEnum(values.LogFormatType)
	raw["CrossplayPlatforms"] = writeRawOrString(values.CrossplayPlatforms)
	raw["bIsUseBackupSaveData"] = writeBool(values.IsUseBackupSaveData)
	raw["bIsShowJoinLeftMessage"] = writeBool(values.IsShowJoinLeftMessage)
	raw["SupplyDropSpan"] = writeInt(values.SupplyDropSpan)
	raw["EnablePredatorBossPal"] = writeBool(values.EnablePredatorBossPal)
	raw["MaxBuildingLimitNum"] = writeInt(values.MaxBuildingLimitNum)
	raw["ServerReplicatePawnCullDistance"] = writeFloat(values.ServerReplicatePawnCullDistance)
	raw["bAllowGlobalPalboxExport"] = writeBool(values.AllowGlobalPalboxExport)
	raw["bAllowGlobalPalboxImport"] = writeBool(values.AllowGlobalPalboxImport)
	raw["EquipmentDurabilityDamageRate"] = writeFloat(values.EquipmentDurabilityDamageRate)
	raw["ItemContainerForceMarkDirtyInterval"] = writeFloat(values.ItemContainerForceMarkDirtyInterval)
	raw["ItemCorruptionMultiplier"] = writeFloat(values.ItemCorruptionMultiplier)
	raw["DenyTechnologyList"] = writeRawOrString(values.DenyTechnologyList)
	raw["GuildRejoinCooldownMinutes"] = writeInt(values.GuildRejoinCooldownMinutes)
	raw["BlockRespawnTime"] = writeFloat(values.BlockRespawnTime)
	raw["RespawnPenaltyDurationThreshold"] = writeFloat(values.RespawnPenaltyDurationThreshold)
	raw["RespawnPenaltyTimeScale"] = writeFloat(values.RespawnPenaltyTimeScale)
	raw["bDisplayPvPItemNumOnWorldMap_BaseCamp"] = writeBool(values.DisplayPvPItemNumOnWorldMapBaseCamp)
	raw["bDisplayPvPItemNumOnWorldMap_Player"] = writeBool(values.DisplayPvPItemNumOnWorldMapPlayer)
	raw["AdditionalDropItemWhenPlayerKillingInPvPMode"] = writeString(values.AdditionalDropItemWhenPlayerKillingInPvPMode)
	raw["AdditionalDropItemNumWhenPlayerKillingInPvPMode"] = writeInt(values.AdditionalDropItemNumWhenPlayerKillingInPvPMode)
	raw["bAdditionalDropItemWhenPlayerKillingInPvPMode"] = writeBool(values.AdditionalDropItemWhenPlayerKillingInPvPEnabled)
	raw["bAllowEnhanceStat_Health"] = writeBool(values.AllowEnhanceStatHealth)
	raw["bAllowEnhanceStat_Attack"] = writeBool(values.AllowEnhanceStatAttack)
	raw["bAllowEnhanceStat_Stamina"] = writeBool(values.AllowEnhanceStatStamina)
	raw["bAllowEnhanceStat_Weight"] = writeBool(values.AllowEnhanceStatWeight)
	raw["bAllowEnhanceStat_WorkSpeed"] = writeBool(values.AllowEnhanceStatWorkSpeed)
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
	written := 0
	for _, key := range order {
		value, ok := raw[key]
		if !ok {
			continue
		}
		if written > 0 {
			inner.WriteByte(',')
		}
		inner.WriteString(key)
		inner.WriteByte('=')
		inner.WriteString(value)
		written++
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
	fields := configFieldDefs()
	keys := make([]string, 0, len(fields))
	for _, field := range fields {
		keys = append(keys, field.RawKey)
	}
	return keys
}

func floatPtr(value float64) *float64 {
	return &value
}

func enumOptions(values ...string) []palConfigFieldOption {
	options := make([]palConfigFieldOption, 0, len(values))
	for _, value := range values {
		options = append(options, palConfigFieldOption{Label: value, Value: value})
	}
	return options
}

func configField(key, rawKey, label, group, typ, description string) palConfigFieldDef {
	return palConfigFieldDef{Key: key, RawKey: rawKey, Label: label, Group: group, Type: typ, Description: description}
}

func configNumberField(key, rawKey, label, group, typ, description string, min, max *float64, step float64) palConfigFieldDef {
	field := configField(key, rawKey, label, group, typ, description)
	field.Min = min
	field.Max = max
	field.Step = floatPtr(step)
	return field
}

func configEnumField(key, rawKey, label, group, description string, options ...string) palConfigFieldDef {
	field := configField(key, rawKey, label, group, "enum", description)
	field.Options = enumOptions(options...)
	return field
}

func configFieldDefs() []palConfigFieldDef {
	rateMin := floatPtr(0.1)
	zero := floatPtr(0)
	one := floatPtr(1)
	portMin := floatPtr(1)
	portMax := floatPtr(65535)
	return []palConfigFieldDef{
		configEnumField("difficulty", "Difficulty", "难度", "基础设置", "服务器难度预设。当前默认样本使用 None。", "None", "Easy", "Normal", "Hard"),
		configEnumField("randomizer_type", "RandomizerType", "帕鲁随机化模式", "世界与事件", "帕鲁刷新随机化模式：None 不随机，Region 按区域随机，All 全局随机。", "None", "Region", "All"),
		configField("randomizer_seed", "RandomizerSeed", "随机化种子", "世界与事件", "string", "启用帕鲁随机化时使用的种子。"),
		configField("is_randomizer_pal_level_random", "bIsRandomizerPalLevelRandom", "随机化帕鲁等级", "世界与事件", "bool", "启用后野生帕鲁等级完全随机；关闭时按区域预期范围随机。"),
		configNumberField("day_time_speed_rate", "DayTimeSpeedRate", "白天速度", "世界与事件", "float", "白天时间推进速度倍率。", rateMin, nil, 0.1),
		configNumberField("night_time_speed_rate", "NightTimeSpeedRate", "夜晚速度", "世界与事件", "float", "夜晚时间推进速度倍率。", rateMin, nil, 0.1),
		configNumberField("exp_rate", "ExpRate", "经验倍率", "倍率", "float", "经验获取倍率。", rateMin, nil, 0.1),
		configNumberField("pal_capture_rate", "PalCaptureRate", "捕获倍率", "倍率", "float", "帕鲁捕获成功率倍率。", rateMin, nil, 0.1),
		configNumberField("pal_spawn_num_rate", "PalSpawnNumRate", "帕鲁刷新倍率", "倍率", "float", "野生帕鲁生成数量倍率，会影响性能。", rateMin, nil, 0.1),
		configNumberField("pal_damage_rate_attack", "PalDamageRateAttack", "帕鲁攻击倍率", "战斗与生存", "float", "帕鲁造成伤害倍率。", rateMin, nil, 0.1),
		configNumberField("pal_damage_rate_defense", "PalDamageRateDefense", "帕鲁受伤倍率", "战斗与生存", "float", "帕鲁受到伤害倍率。", rateMin, nil, 0.1),
		configNumberField("player_damage_rate_attack", "PlayerDamageRateAttack", "玩家攻击倍率", "战斗与生存", "float", "玩家造成伤害倍率。", rateMin, nil, 0.1),
		configNumberField("player_damage_rate_defense", "PlayerDamageRateDefense", "玩家受伤倍率", "战斗与生存", "float", "玩家受到伤害倍率。", rateMin, nil, 0.1),
		configNumberField("player_stomach_decrease_rate", "PlayerStomachDecreaceRate", "玩家饥饿消耗", "战斗与生存", "float", "玩家饱食度消耗倍率。", rateMin, nil, 0.1),
		configNumberField("player_stamina_decrease_rate", "PlayerStaminaDecreaceRate", "玩家体力消耗", "战斗与生存", "float", "玩家体力消耗倍率。", rateMin, nil, 0.1),
		configNumberField("player_auto_hp_regen_rate", "PlayerAutoHPRegeneRate", "玩家自然回血", "战斗与生存", "float", "玩家自然生命恢复倍率。", rateMin, nil, 0.1),
		configNumberField("player_auto_hp_regen_rate_in_sleep", "PlayerAutoHpRegeneRateInSleep", "玩家睡眠回血", "战斗与生存", "float", "玩家睡眠时生命恢复倍率。", rateMin, nil, 0.1),
		configNumberField("pal_stomach_decrease_rate", "PalStomachDecreaceRate", "帕鲁饥饿消耗", "战斗与生存", "float", "帕鲁饱食度消耗倍率。", rateMin, nil, 0.1),
		configNumberField("pal_stamina_decrease_rate", "PalStaminaDecreaceRate", "帕鲁体力消耗", "战斗与生存", "float", "帕鲁体力消耗倍率。", rateMin, nil, 0.1),
		configNumberField("pal_auto_hp_regen_rate", "PalAutoHPRegeneRate", "帕鲁自然回血", "战斗与生存", "float", "帕鲁自然生命恢复倍率。", rateMin, nil, 0.1),
		configNumberField("pal_auto_hp_regen_rate_in_sleep", "PalAutoHpRegeneRateInSleep", "帕鲁睡眠回血", "战斗与生存", "float", "帕鲁在 Palbox/睡眠时生命恢复倍率。", rateMin, nil, 0.1),
		configNumberField("build_object_hp_rate", "BuildObjectHpRate", "建筑生命倍率", "据点与建筑", "float", "建筑物生命值倍率。", rateMin, nil, 0.1),
		configNumberField("build_object_damage_rate", "BuildObjectDamageRate", "建筑受伤倍率", "据点与建筑", "float", "建筑物受到伤害倍率。", rateMin, nil, 0.1),
		configNumberField("build_object_deterioration_damage_rate", "BuildObjectDeteriorationDamageRate", "建筑退化倍率", "据点与建筑", "float", "建筑物自然损耗/退化速度倍率。", zero, nil, 0.1),
		configNumberField("collection_drop_rate", "CollectionDropRate", "采集掉落倍率", "倍率", "float", "采集获得物品数量倍率。", rateMin, nil, 0.1),
		configNumberField("collection_object_hp_rate", "CollectionObjectHpRate", "采集物生命倍率", "倍率", "float", "可采集对象生命值倍率。", rateMin, nil, 0.1),
		configNumberField("collection_object_respawn_speed_rate", "CollectionObjectRespawnSpeedRate", "采集物刷新速度", "倍率", "float", "可采集对象刷新间隔倍率。", rateMin, nil, 0.1),
		configNumberField("enemy_drop_item_rate", "EnemyDropItemRate", "敌人掉落倍率", "倍率", "float", "敌人掉落物品数量倍率。", rateMin, nil, 0.1),
		configEnumField("death_penalty", "DeathPenalty", "死亡惩罚", "战斗与生存", "None 不掉落，Item 掉落非装备物品，ItemAndEquipment 掉落所有物品，All 掉落物品和队伍帕鲁。", "None", "Item", "ItemAndEquipment", "All"),
		configField("enable_player_to_player_damage", "bEnablePlayerToPlayerDamage", "玩家互伤", "PvP", "bool", "允许玩家对其他玩家造成伤害。"),
		configField("enable_friendly_fire", "bEnableFriendlyFire", "友军伤害", "PvP", "bool", "允许友方单位/同阵营伤害。"),
		configField("enable_invader_enemy", "bEnableInvaderEnemy", "入侵事件", "世界与事件", "bool", "启用据点入侵事件。"),
		configField("active_unko", "bActiveUNKO", "UNKO 掉落", "兼容/保留", "bool", "官方未公开详细用途的兼容字段，默认关闭。"),
		configField("enable_aim_assist_pad", "bEnableAimAssistPad", "手柄瞄准辅助", "功能开关", "bool", "启用手柄瞄准辅助。"),
		configField("enable_aim_assist_keyboard", "bEnableAimAssistKeyboard", "键鼠瞄准辅助", "功能开关", "bool", "启用键鼠瞄准辅助。"),
		configNumberField("drop_item_max_num", "DropItemMaxNum", "掉落物上限", "世界与事件", "int", "世界中同时存在的掉落物数量上限。", one, nil, 1),
		configNumberField("drop_item_max_num_unko", "DropItemMaxNum_UNKO", "UNKO 掉落物上限", "兼容/保留", "int", "官方未公开详细用途的兼容字段，控制 UNKO 掉落物数量上限。", one, nil, 1),
		configNumberField("base_camp_max_num", "BaseCampMaxNum", "全服据点总数", "据点与建筑", "int", "服务器允许存在的据点总数。", one, nil, 1),
		configNumberField("base_camp_worker_max_num", "BaseCampWorkerMaxNum", "据点工作帕鲁数", "性能", "int", "每个据点的工作帕鲁上限，官方最大值 50。", one, floatPtr(50), 1),
		configNumberField("drop_item_alive_max_hours", "DropItemAliveMaxHours", "掉落物保留小时", "世界与事件", "float", "掉落物在世界中保留的最长时间。", zero, nil, 0.5),
		configField("auto_reset_guild_no_online_players", "bAutoResetGuildNoOnlinePlayers", "离线公会自动清理", "据点与建筑", "bool", "公会无人上线达到指定时间后，自动删除建筑和据点帕鲁。"),
		configNumberField("auto_reset_guild_time_no_online_players", "AutoResetGuildTimeNoOnlinePlayers", "离线清理时间", "据点与建筑", "float", "触发离线公会自动清理所需的离线小时数。", zero, nil, 1),
		configNumberField("guild_player_max_num", "GuildPlayerMaxNum", "公会人数上限", "据点与建筑", "int", "单个公会最大玩家数。", one, nil, 1),
		configNumberField("base_camp_max_num_in_guild", "BaseCampMaxNumInGuild", "公会据点上限", "性能", "int", "每个公会最大据点数，官方默认 4、最大 10。", one, floatPtr(10), 1),
		configNumberField("egg_default_hatching_time", "PalEggDefaultHatchingTime", "巨大蛋孵化小时", "世界与事件", "float", "巨大蛋默认孵化时间；其他蛋也会按规则需要孵化时间。", zero, nil, 1),
		configNumberField("work_speed_rate", "WorkSpeedRate", "工作速度倍率", "倍率", "float", "工作速度倍率。", rateMin, nil, 0.1),
		configNumberField("auto_save_span", "AutoSaveSpan", "自动保存间隔", "基础设置", "float", "服务器自动保存间隔，单位秒。", rateMin, nil, 1),
		configField("is_multiplay", "bIsMultiplay", "多人模式", "兼容/保留", "bool", "默认配置保留字段；专服通常保持默认。"),
		configField("is_pvp", "bIsPvP", "PvP 模式", "PvP", "bool", "启用 PvP。完整 PvP 通常还需要玩家互伤和其他公会防御伤害。"),
		configField("hardcore", "bHardcore", "硬核模式", "战斗与生存", "bool", "启用后死亡不可正常复活。"),
		configField("pal_lost", "bPalLost", "死亡丢失帕鲁", "战斗与生存", "bool", "死亡时永久失去帕鲁。"),
		configField("character_recreate_in_hardcore", "bCharacterRecreateInHardcore", "硬核可重建角色", "战斗与生存", "bool", "硬核模式死亡后是否允许重新创建角色。"),
		configField("can_pickup_other_guild_death_penalty_drop", "bCanPickupOtherGuildDeathPenaltyDrop", "可拾取其他公会死亡掉落", "PvP", "bool", "允许拾取其他公会玩家死亡惩罚掉落。"),
		configField("enable_non_login_penalty", "bEnableNonLoginPenalty", "未登录惩罚", "功能开关", "bool", "启用玩家长期未登录相关惩罚。"),
		configField("enable_fast_travel", "bEnableFastTravel", "快速传送", "功能开关", "bool", "启用快速传送。"),
		configField("enable_fast_travel_only_base_camp", "bEnableFastTravelOnlyBaseCamp", "仅据点间传送", "功能开关", "bool", "限制快速传送只能在据点之间进行。"),
		configField("is_start_location_select_by_map", "bIsStartLocationSelectByMap", "地图选择出生点", "功能开关", "bool", "允许玩家在地图上选择初始出生位置。"),
		configField("exist_player_after_logout", "bExistPlayerAfterLogout", "离线角色留存", "功能开关", "bool", "玩家登出后角色是否在当前位置进入睡眠状态。"),
		configField("enable_defense_other_guild_player", "bEnableDefenseOtherGuildPlayer", "据点防御伤害其他公会", "PvP", "bool", "允许据点防御对其他公会玩家生效。"),
		configField("invisible_other_guild_base_camp_area_fx", "bInvisibleOtherGuildBaseCampAreaFX", "隐藏其他公会据点范围", "功能开关", "bool", "控制是否显示其他公会据点区域边界效果。"),
		configField("build_area_limit", "bBuildAreaLimit", "限制特殊区域建造", "据点与建筑", "bool", "限制在快速传送点等结构附近建造。"),
		configNumberField("item_weight_rate", "ItemWeightRate", "物品重量倍率", "战斗与生存", "float", "物品重量倍率。", rateMin, nil, 0.1),
		configNumberField("coop_player_max_num", "CoopPlayerMaxNum", "合作人数上限", "基础设置", "int", "非专服合作模式人数上限；专服通常不使用。", one, nil, 1),
		configNumberField("server_player_max_num", "ServerPlayerMaxNum", "服务器玩家上限", "基础设置", "int", "可加入服务器的最大玩家数。", one, floatPtr(64), 1),
		configField("server_name", "ServerName", "服务器名称", "基础设置", "string", "服务器列表中显示的名称。"),
		configField("server_description", "ServerDescription", "服务器描述", "基础设置", "textarea", "服务器描述。"),
		configField("admin_password", "AdminPassword", "管理员密码", "基础设置", "password", "用于在服务器上取得管理员权限的密码。"),
		configField("server_password", "ServerPassword", "服务器密码", "基础设置", "password", "玩家加入服务器所需密码，留空表示不需要。"),
		configField("allow_client_mod", "bAllowClientMod", "允许客户端 MOD", "功能开关", "bool", "允许启用 MOD 的玩家加入服务器。"),
		configNumberField("public_port", "PublicPort", "公网端口", "网络与管理", "int", "社区服务器对外展示端口；不改变实际监听端口。", portMin, portMax, 1),
		configField("public_ip", "PublicIP", "公网 IP", "网络与管理", "string", "社区服务器显式指定公网 IP，留空自动检测。"),
		configField("rcon_enabled", "RCONEnabled", "RCON", "网络与管理", "bool", "启用 RCON。官方已标记 RCON 为弃用，优先使用 REST API。"),
		configNumberField("rcon_port", "RCONPort", "RCON 端口", "网络与管理", "int", "RCON 使用的 TCP 端口。", portMin, portMax, 1),
		configField("region", "Region", "区域", "兼容/保留", "string", "默认配置保留字段；通常留空。"),
		configField("use_auth", "bUseAuth", "启用认证", "网络与管理", "bool", "启用服务器认证相关逻辑，默认开启。"),
		configField("ban_list_url", "BanListURL", "封禁列表 URL", "网络与管理", "string", "服务器读取的官方/远端封禁列表地址。"),
		configField("rest_api_enabled", "RESTAPIEnabled", "REST API", "网络与管理", "bool", "启用 Palworld REST API。不要直接暴露到公网。"),
		configNumberField("rest_api_port", "RESTAPIPort", "REST API 端口", "网络与管理", "int", "REST API 监听端口。", portMin, portMax, 1),
		configField("show_player_list", "bShowPlayerList", "ESC 玩家列表", "网络与管理", "bool", "在 ESC 菜单显示玩家列表。"),
		configNumberField("chat_post_limit_per_minute", "ChatPostLimitPerMinute", "每分钟聊天上限", "网络与管理", "int", "每名玩家每分钟允许发送的聊天消息数量。", zero, nil, 1),
		configField("crossplay_platforms", "CrossplayPlatforms", "跨平台连接", "网络与管理", "raw", "允许连接的平台列表，例如 (Steam,Xbox,PS5,Mac)。"),
		configField("is_use_backup_save_data", "bIsUseBackupSaveData", "官方存档备份", "基础设置", "bool", "启用官方世界备份；会增加磁盘负载。"),
		configEnumField("log_format_type", "LogFormatType", "日志格式", "网络与管理", "服务器日志格式。", "Text", "Json"),
		configField("is_show_join_left_message", "bIsShowJoinLeftMessage", "加入离开提示", "网络与管理", "bool", "在专服内显示玩家加入/离开消息。"),
		configNumberField("supply_drop_span", "SupplyDropSpan", "陨石/补给间隔", "世界与事件", "int", "陨石或补给掉落事件间隔，单位分钟。", zero, nil, 1),
		configField("enable_predator_boss_pal", "EnablePredatorBossPal", "掠食者 Boss 帕鲁", "世界与事件", "bool", "启用掠食者 Boss 帕鲁。"),
		configNumberField("max_building_limit_num", "MaxBuildingLimitNum", "每玩家建筑上限", "性能", "int", "每名玩家建筑数量上限，0 表示无限制。", zero, nil, 100),
		configNumberField("server_replicate_pawn_cull_distance", "ServerReplicatePawnCullDistance", "帕鲁同步距离", "性能", "float", "玩家周围帕鲁同步距离，单位厘米；官方范围 5000 到 15000。", floatPtr(5000), floatPtr(15000), 500),
		configField("allow_global_palbox_export", "bAllowGlobalPalboxExport", "允许导出全局 Palbox", "功能开关", "bool", "允许保存到 Global Palbox。"),
		configField("allow_global_palbox_import", "bAllowGlobalPalboxImport", "允许导入全局 Palbox", "功能开关", "bool", "允许从 Global Palbox 加载。"),
		configNumberField("equipment_durability_damage_rate", "EquipmentDurabilityDamageRate", "装备耐久损耗", "战斗与生存", "float", "装备耐久损耗倍率。", rateMin, nil, 0.1),
		configNumberField("item_container_force_mark_dirty_interval", "ItemContainerForceMarkDirtyInterval", "容器强制同步间隔", "性能", "float", "打开容器 UI 时强制重新同步的间隔秒数。", rateMin, nil, 0.1),
		configNumberField("item_corruption_multiplier", "ItemCorruptionMultiplier", "物品腐坏倍率", "战斗与生存", "float", "物品腐坏速度倍率。", zero, nil, 0.1),
		configField("deny_technology_list", "DenyTechnologyList", "禁用科技列表", "功能开关", "raw", "禁用指定科技 ID，例如 (\"PALBOX\",\"RepairBench\")。"),
		configNumberField("guild_rejoin_cooldown_minutes", "GuildRejoinCooldownMinutes", "重新加入公会冷却", "据点与建筑", "int", "离开后重新加入公会的冷却分钟数。", zero, nil, 1),
		configNumberField("block_respawn_time", "BlockRespawnTime", "复活冷却", "战斗与生存", "float", "死亡后可复活前的冷却秒数。", zero, nil, 1),
		configNumberField("respawn_penalty_duration_threshold", "RespawnPenaltyDurationThreshold", "复活惩罚阈值", "战斗与生存", "float", "再次死亡时触发复活冷却倍率的生存时长阈值，单位秒。", zero, nil, 1),
		configNumberField("respawn_penalty_time_scale", "RespawnPenaltyTimeScale", "复活惩罚倍率", "战斗与生存", "float", "应用到复活冷却的倍率。", rateMin, nil, 0.1),
		configField("display_pvp_item_num_on_world_map_base_camp", "bDisplayPvPItemNumOnWorldMap_BaseCamp", "地图显示据点 PvP 物品数", "PvP", "bool", "在地图上显示各据点的 PvP 专属物品数量。"),
		configField("display_pvp_item_num_on_world_map_player", "bDisplayPvPItemNumOnWorldMap_Player", "地图显示玩家 PvP 物品数", "PvP", "bool", "在地图上显示玩家位置和 PvP 专属物品数量。"),
		configField("additional_drop_item_when_player_killing_in_pvp_mode", "AdditionalDropItemWhenPlayerKillingInPvPMode", "PvP 击杀额外掉落物", "PvP", "string", "启用 PvP 击杀额外掉落后掉落的物品 ID。"),
		configNumberField("additional_drop_item_num_when_player_killing_in_pvp_mode", "AdditionalDropItemNumWhenPlayerKillingInPvPMode", "PvP 击杀额外掉落数量", "PvP", "int", "启用 PvP 击杀额外掉落后掉落的物品数量。", one, nil, 1),
		configField("additional_drop_item_when_player_killing_in_pvp_enabled", "bAdditionalDropItemWhenPlayerKillingInPvPMode", "启用 PvP 击杀额外掉落", "PvP", "bool", "玩家在 PvP 中被击杀时是否掉落指定额外物品。"),
		configField("allow_enhance_stat_health", "bAllowEnhanceStat_Health", "允许强化生命", "属性强化", "bool", "允许把属性点分配到生命。"),
		configField("allow_enhance_stat_attack", "bAllowEnhanceStat_Attack", "允许强化攻击", "属性强化", "bool", "允许把属性点分配到攻击。"),
		configField("allow_enhance_stat_stamina", "bAllowEnhanceStat_Stamina", "允许强化体力", "属性强化", "bool", "允许把属性点分配到体力。"),
		configField("allow_enhance_stat_weight", "bAllowEnhanceStat_Weight", "允许强化负重", "属性强化", "bool", "允许把属性点分配到负重。"),
		configField("allow_enhance_stat_work_speed", "bAllowEnhanceStat_WorkSpeed", "允许强化工作速度", "属性强化", "bool", "允许把属性点分配到工作速度。"),
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

func readFloatWithFallback(raw map[string]string, primaryKey, fallbackKey string, fallback float64) float64 {
	if _, ok := raw[primaryKey]; ok {
		return readFloat(raw, primaryKey, fallback)
	}
	return readFloat(raw, fallbackKey, fallback)
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
		return ""
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
