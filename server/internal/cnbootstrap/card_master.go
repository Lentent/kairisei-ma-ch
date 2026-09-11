package cnbootstrap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"kairisei.local/server/internal/release"
)

const maxCNCardMasterBytes = 8 * 1024 * 1024

type cnCardRuntimeMaster struct {
	SchemaVersion                     int                                 `json:"schema_version"`
	ClientProfile                     string                              `json:"client_profile"`
	CardProgressionConfigVersion      int                                 `json:"card_progression_config_version"`
	CardProgressionPolicy             release.CardProgressionPolicy       `json:"card_progression_policy"`
	CardExperienceTables              map[int][]int                       `json:"card_experience_tables"`
	SphereConfigVersion               int                                 `json:"sphere_config_version"`
	SphereProgressionPolicy           release.SphereProgressionPolicy     `json:"sphere_progression_policy"`
	BuddyConfigVersion                int                                 `json:"buddy_config_version"`
	BuddyProgressionPolicy            release.BuddyProgressionPolicy      `json:"buddy_progression_policy"`
	SupportDeckConfigVersion          int                                 `json:"support_deck_config_version"`
	SupportDeckSetCardNum             int                                 `json:"support_deck_set_card_num"`
	Source                            json.RawMessage                     `json:"source"`
	CardTemplates                     []release.Card                      `json:"card_templates"`
	CardCollectionPages               [][]int                             `json:"card_collection_pages"`
	DeckRankPolicy                    release.DeckRankPolicy              `json:"deck_rank_policy"`
	StackCardTemplates                []release.CardStack                 `json:"stack_card_templates"`
	CardCategoryProfiles              []release.CardCategoryProfile       `json:"card_category_profiles"`
	CardGroupProfiles                 []release.CardGroupProfile          `json:"card_group_profiles"`
	CardGroupPolicy                   release.CardGroupPolicy             `json:"card_group_policy"`
	EvolutionTransitions              []release.EvolutionTransition       `json:"evolution_transitions"`
	CardDevelopmentPolicy             release.CardDevelopmentPolicy       `json:"card_development_policy"`
	SkippedIncompleteEvolutionCardIDs []int                               `json:"skipped_incomplete_evolution_source_cardids"`
	SphereDefinitions                 []release.SphereDefinition          `json:"sphere_definitions"`
	SphereExperienceTables            map[int][]int                       `json:"sphere_experience_tables"`
	SphereEvolutionPrices             map[string][]int                    `json:"sphere_evolution_prices"`
	SphereSeedTemplates               []release.Sphere                    `json:"sphere_seed_templates"`
	SphereSeedDecks                   map[string][]int64                  `json:"sphere_seed_decks"`
	BuddyDefinitions                  []release.BuddyDefinition           `json:"buddy_definitions"`
	BuddyExperienceTables             map[int][]int                       `json:"buddy_experience_tables"`
	BuddyEvolutionPrices              map[string][]int                    `json:"buddy_evolution_prices"`
	BuddySeedTemplates                []release.Buddy                     `json:"buddy_seed_templates"`
	BuddySeedDecks                    map[string][]int64                  `json:"buddy_seed_decks"`
	BuddySeedStackCards               []release.CardStack                 `json:"buddy_seed_stack_cards"`
	SupportDeckSlotUnlockRules        []release.SupportDeckSlotUnlockRule `json:"support_deck_slot_unlock_rules"`
}

func loadCNCardRuntimeMaster(masterPath string) (cnCardRuntimeMaster, error) {
	if masterPath == "" {
		return cnCardRuntimeMaster{}, errors.New("CN card runtime master path is required")
	}
	absolute, err := filepath.Abs(masterPath)
	if err != nil {
		return cnCardRuntimeMaster{}, fmt.Errorf("resolve CN card runtime master: %w", err)
	}
	file, err := os.Open(absolute)
	if err != nil {
		return cnCardRuntimeMaster{}, fmt.Errorf("open CN card runtime master: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return cnCardRuntimeMaster{}, fmt.Errorf("stat CN card runtime master: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxCNCardMasterBytes {
		return cnCardRuntimeMaster{}, errors.New("CN card runtime master must be a non-empty regular JSON file within the size limit")
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxCNCardMasterBytes+1))
	decoder.DisallowUnknownFields()
	var master cnCardRuntimeMaster
	if err := decoder.Decode(&master); err != nil {
		return cnCardRuntimeMaster{}, fmt.Errorf("decode CN card runtime master: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return cnCardRuntimeMaster{}, err
	}
	if err := validateCNCardRuntimeMaster(master); err != nil {
		return cnCardRuntimeMaster{}, err
	}
	return master, nil
}

func validateCNCardRuntimeMaster(master cnCardRuntimeMaster) error {
	if err := validateCNCardCollection(master); err != nil {
		return err
	}
	if err := validateCNDeckRankPolicy(master.DeckRankPolicy, master.CardTemplates); err != nil {
		return err
	}
	if master.SchemaVersion != 15 || master.ClientProfile != "cn602-bootstrap" ||
		master.CardProgressionConfigVersion <= 0 ||
		master.CardProgressionPolicy.ConfigVersion != master.CardProgressionConfigVersion ||
		master.CardProgressionPolicy.FusionGoldPerMaterialPerBaseLevel <= 0 ||
		master.CardProgressionPolicy.MaximumCardMaterialCount != 100 ||
		master.SphereConfigVersion <= 0 ||
		master.SphereProgressionPolicy.ConfigVersion != master.SphereConfigVersion ||
		master.SphereProgressionPolicy.MaterialLevelBonusPermillePerLevel <= 0 ||
		master.BuddyConfigVersion <= 0 ||
		master.BuddyProgressionPolicy.ConfigVersion != master.BuddyConfigVersion ||
		master.BuddyProgressionPolicy.MaximumMaterialCount <= 0 ||
		master.SupportDeckConfigVersion <= 0 || master.SupportDeckSetCardNum <= 0 ||
		master.SupportDeckSetCardNum > 10 {
		return errors.New("CN card runtime master identity is invalid")
	}
	if len(master.Source) == 0 || bytes.Equal(master.Source, []byte("null")) ||
		len(master.CardTemplates) == 0 || len(master.StackCardTemplates) == 0 ||
		len(master.CardCategoryProfiles) != 1 || len(master.CardGroupProfiles) != 6 ||
		len(master.CardExperienceTables) == 0 ||
		len(master.EvolutionTransitions) == 0 || len(master.SphereDefinitions) == 0 ||
		len(master.SphereExperienceTables) == 0 || len(master.SphereEvolutionPrices) == 0 ||
		len(master.SphereSeedTemplates) == 0 || len(master.SphereSeedDecks) != 4 ||
		len(master.BuddyDefinitions) == 0 || len(master.BuddyExperienceTables) == 0 ||
		len(master.BuddyEvolutionPrices) == 0 || len(master.BuddySeedTemplates) == 0 ||
		len(master.BuddySeedDecks) != 4 || len(master.BuddySeedStackCards) != 2 ||
		len(master.SupportDeckSlotUnlockRules) != master.SupportDeckSetCardNum {
		return errors.New("CN card runtime master is incomplete")
	}
	if err := validateCNCardGroupProfiles(master); err != nil {
		return err
	}
	if !validCNCardProgressionSourceState(master.CardProgressionPolicy) {
		return errors.New("CN card progression source state is invalid")
	}
	if err := validateCNCardFusionSuccessPolicy(master.CardProgressionPolicy); err != nil {
		return err
	}
	if !validCNSphereProgressionSourceState(master.SphereProgressionPolicy) {
		return errors.New("CN sphere progression source state is invalid")
	}
	if !validCNBuddyProgressionSourceState(master.BuddyProgressionPolicy) {
		return errors.New("CN Buddy progression source state is invalid")
	}
	if master.CardDevelopmentPolicy.TimeEveryFameSeconds <= 0 ||
		master.CardDevelopmentPolicy.CoinEveryHour <= 0 ||
		master.CardDevelopmentPolicy.HelpPath == "" || master.CardDevelopmentPolicy.HelpPath[0] != '/' ||
		master.CardDevelopmentPolicy.Evidence != "PLACEHOLDER" {
		return errors.New("CN card development local policy is invalid")
	}
	for index, rule := range master.SupportDeckSlotUnlockRules {
		if rule.SlotIndex != index+1 || rule.Level <= 0 ||
			rule.CardCollectionNum <= 0 || rule.Gold <= 0 {
			return fmt.Errorf("invalid CN support-deck slot rule %d", index+1)
		}
	}
	cardIDs := make(map[int]struct{}, len(master.CardTemplates))
	for _, card := range master.CardTemplates {
		if card.FusionAttributes > 31 {
			return fmt.Errorf("invalid CN fusion attribute mask for card %d", card.CardID)
		}
		if card.UniqueID != 0 || card.CardID <= 0 || card.Name == "" ||
			card.SameCardID <= 0 || card.SameSupportCardID <= 0 ||
			card.RarityRank < 1 || card.RarityRank > 8 ||
			card.Level != 1 || card.LevelMax < 1 || card.ExperienceTableID < 0 ||
			card.Experience != 0 || card.NowLevelExperience != 0 ||
			card.Love != 0 || card.LoveMax < 0 ||
			card.Fame != 1 || card.FameMax < 1 || card.FameMax < card.Fame ||
			card.DevelopmentType < 0 || card.DevelopmentType > 2 ||
			card.DecomposeRadix < 0 || card.DevelopRadix < 0 ||
			(card.DevelopmentType == 1 && card.DecomposeRadix == 0) ||
			(card.DevelopmentType == 2 && card.DevelopRadix == 0) ||
			card.NextLevelExperience < 0 || card.AddExperience <= 0 || card.BaseAddPrice <= 0 ||
			card.SellGold < 0 ||
			card.SkillLevelMax != 1 || len(card.SkillLevels) != 1 || card.SkillLevels[0] != 1 {
			return fmt.Errorf("invalid CN card template %d", card.CardID)
		}
		values, exists := master.CardExperienceTables[card.ExperienceTableID]
		if card.LevelMax > 1 && (!exists || len(values) < card.LevelMax-1) {
			return fmt.Errorf("CN card %d experience table is incomplete", card.CardID)
		}
		normalized, err := normalizeCNCard(card, card, values, master.CardProgressionPolicy, false)
		if err != nil || !reflect.DeepEqual(normalized, card) {
			return fmt.Errorf("CN card template %d derived fields are invalid", card.CardID)
		}
		if _, exists := cardIDs[card.CardID]; exists {
			return fmt.Errorf("duplicate CN card template %d", card.CardID)
		}
		cardIDs[card.CardID] = struct{}{}
	}
	stackIDs := make(map[int]release.CardStack, len(master.StackCardTemplates))
	for _, card := range master.StackCardTemplates {
		if card.CardID <= 0 || card.Num != 0 || card.AddExperience <= 0 || card.BaseAddPrice < 0 || card.MaterialType < 0 || card.MaterialType > 10 {
			return fmt.Errorf("invalid CN stack-card template %d", card.CardID)
		}
		if _, exists := stackIDs[card.CardID]; exists {
			return fmt.Errorf("duplicate CN stack-card template %d", card.CardID)
		}
		stackIDs[card.CardID] = card
	}
	for cardID, bonuses := range master.CardProgressionPolicy.FameMaterials {
		if _, exists := stackIDs[cardID]; !exists {
			return fmt.Errorf("unknown CN fame material %d", cardID)
		}
		for _, bonus := range bonuses {
			if bonus < 0 || bonus > math.MaxInt32/master.CardProgressionPolicy.MaximumCardMaterialCount {
				return fmt.Errorf("invalid CN fame material bonus %d", cardID)
			}
		}
	}
	transitions := make(map[[2]int]struct{}, len(master.EvolutionTransitions))
	for _, transition := range master.EvolutionTransitions {
		key := [2]int{transition.FromCardID, transition.ToCardID}
		if transition.Type < 0 || transition.Type > 3 ||
			transition.Gold < 0 || len(transition.Materials) == 0 {
			return fmt.Errorf("invalid CN evolution transition %v", key)
		}
		if _, exists := cardIDs[transition.FromCardID]; !exists {
			return fmt.Errorf("unknown CN evolution source card %d", transition.FromCardID)
		}
		if _, exists := cardIDs[transition.ToCardID]; !exists {
			return fmt.Errorf("unknown CN evolution target card %d", transition.ToCardID)
		}
		if _, exists := transitions[key]; exists {
			return fmt.Errorf("duplicate CN evolution transition %v", key)
		}
		transitions[key] = struct{}{}
		materialIDs := make(map[int]struct{}, len(transition.Materials))
		for _, material := range transition.Materials {
			if material.Num <= 0 || material.Fame < 0 || (transition.Type == 1 && material.Fame == 0) {
				return fmt.Errorf("invalid CN evolution material %d", material.CardID)
			}
			_, isStackCard := stackIDs[material.CardID]
			_, isInstanceCard := cardIDs[material.CardID]
			if !isStackCard && !isInstanceCard {
				return fmt.Errorf("unknown CN evolution material %d", material.CardID)
			}
			if _, exists := materialIDs[material.CardID]; exists {
				return fmt.Errorf("duplicate CN evolution material %d", material.CardID)
			}
			materialIDs[material.CardID] = struct{}{}
		}
	}
	definitions := make(map[int]release.SphereDefinition, len(master.SphereDefinitions))
	for _, definition := range master.SphereDefinitions {
		if definition.SphereID <= 0 || definition.SameSphereID <= 0 || definition.Name == "" ||
			(definition.Type != "NORMAL" && definition.Type != "CHALICE") ||
			definition.MaxLevel <= 0 || definition.EvolutionCount < 0 || definition.Count < 0 ||
			definition.SellGold < 0 || definition.MaterialAddExperience <= 0 || definition.FusionBaseAddPrice <= 0 {
			return fmt.Errorf("invalid CN sphere definition %d", definition.SphereID)
		}
		if _, duplicate := definitions[definition.SphereID]; duplicate {
			return fmt.Errorf("duplicate CN sphere definition %d", definition.SphereID)
		}
		allowed := false
		for _, value := range definition.EquipAllowed {
			allowed = allowed || value
		}
		if !allowed {
			return fmt.Errorf("CN sphere %d has no equipable Arthur", definition.SphereID)
		}
		if definition.MaxLevel > 1 {
			values, exists := master.SphereExperienceTables[definition.ExperienceTableID]
			if !exists || len(values) < definition.MaxLevel-1 {
				return fmt.Errorf("CN sphere %d experience table is incomplete", definition.SphereID)
			}
			for _, value := range values[:definition.MaxLevel-1] {
				if value <= 0 {
					return fmt.Errorf("CN sphere experience table %d has an invalid per-level requirement", definition.ExperienceTableID)
				}
			}
		}
		definitions[definition.SphereID] = definition
	}
	for _, definition := range master.SphereDefinitions {
		if definition.EvolutionID != 0 {
			target, exists := definitions[definition.EvolutionID]
			if !exists || target.SameSphereID != definition.SameSphereID ||
				target.EvolutionCount != definition.EvolutionCount+1 ||
				target.ExperienceTableID != definition.ExperienceTableID ||
				target.MaxLevel < definition.MaxLevel {
				return fmt.Errorf("invalid CN sphere evolution %d -> %d", definition.SphereID, definition.EvolutionID)
			}
		}
	}
	for _, rarity := range []string{"NORMAL", "RARE", "MILLIONRARE"} {
		values, exists := master.SphereEvolutionPrices[rarity]
		if !exists || len(values) != 4 {
			return fmt.Errorf("CN sphere evolution prices for %s are incomplete", rarity)
		}
		for _, value := range values {
			if value < 0 {
				return fmt.Errorf("CN sphere evolution price for %s is negative", rarity)
			}
		}
	}
	seedIDs := make(map[int64]release.Sphere, len(master.SphereSeedTemplates))
	for _, sphere := range master.SphereSeedTemplates {
		definition, exists := definitions[sphere.SphereID]
		if !exists || sphere.UniqueID <= 0 || sphere.Level < 1 || sphere.Level > definition.MaxLevel ||
			sphere.Experience < 0 || sphere.IsLock < 0 || sphere.IsLock > 1 ||
			sphere.BaseAddPrice != definition.FusionBaseAddPrice {
			return fmt.Errorf("invalid CN sphere seed %d", sphere.UniqueID)
		}
		normalized, err := normalizeCNSphere(
			sphere, definition, master.SphereExperienceTables, master.SphereProgressionPolicy,
		)
		if err != nil || normalized != sphere {
			return fmt.Errorf("CN sphere seed %d derived fields are invalid", sphere.UniqueID)
		}
		if _, duplicate := seedIDs[sphere.UniqueID]; duplicate {
			return fmt.Errorf("duplicate CN sphere seed unique ID %d", sphere.UniqueID)
		}
		seedIDs[sphere.UniqueID] = sphere
	}
	for arthurType := 1; arthurType <= 4; arthurType++ {
		uniqueIDs, exists := master.SphereSeedDecks[fmt.Sprint(arthurType)]
		if !exists || len(uniqueIDs) != 3 {
			return fmt.Errorf("CN sphere seed deck %d is incomplete", arthurType)
		}
		families := make(map[int]struct{}, 3)
		for _, uniqueID := range uniqueIDs {
			sphere, owned := seedIDs[uniqueID]
			definition := definitions[sphere.SphereID]
			if !owned || !definition.EquipAllowed[arthurType-1] {
				return fmt.Errorf("CN sphere seed deck %d references unusable sphere %d", arthurType, uniqueID)
			}
			if _, duplicate := families[definition.SameSphereID]; duplicate {
				return fmt.Errorf("CN sphere seed deck %d repeats sphere family %d", arthurType, definition.SameSphereID)
			}
			families[definition.SameSphereID] = struct{}{}
		}
	}
	buddyDefinitions := make(map[int]release.BuddyDefinition, len(master.BuddyDefinitions))
	for _, definition := range master.BuddyDefinitions {
		if definition.BuddyID <= 0 || definition.SameBuddyID <= 0 || definition.Name == "" ||
			definition.EvolutionCount < 0 || definition.MaxLevel <= 0 ||
			definition.SellGold < 0 || definition.MaterialAddExperience <= 0 ||
			definition.FusionBaseAddPrice <= 0 ||
			(definition.OverlimitItemID == 0) != (definition.OverlimitItemNum == 0) {
			return fmt.Errorf("invalid CN buddy definition %d", definition.BuddyID)
		}
		if _, duplicate := buddyDefinitions[definition.BuddyID]; duplicate {
			return fmt.Errorf("duplicate CN buddy definition %d", definition.BuddyID)
		}
		values, exists := master.BuddyExperienceTables[definition.ExperienceTableID]
		if !exists || len(values) < definition.MaxLevel-1 {
			return fmt.Errorf("CN buddy %d experience table is incomplete", definition.BuddyID)
		}
		for _, value := range values[:definition.MaxLevel-1] {
			if value <= 0 {
				return fmt.Errorf("CN buddy experience table %d is invalid", definition.ExperienceTableID)
			}
		}
		buddyDefinitions[definition.BuddyID] = definition
	}
	for _, definition := range master.BuddyDefinitions {
		if definition.EvolutionID != 0 {
			target, exists := buddyDefinitions[definition.EvolutionID]
			if !exists || target.SameBuddyID != definition.SameBuddyID ||
				target.EvolutionCount != definition.EvolutionCount+1 ||
				target.ExperienceTableID != definition.ExperienceTableID ||
				target.MaxLevel < definition.MaxLevel {
				return fmt.Errorf("invalid CN buddy evolution %d -> %d", definition.BuddyID, definition.EvolutionID)
			}
		}
	}
	for _, rarity := range []string{"NORMAL", "RARE", "MILLIONRARE", "EXRARE", "LEGEND"} {
		values, exists := master.BuddyEvolutionPrices[rarity]
		if !exists || len(values) != 5 {
			return fmt.Errorf("CN buddy evolution prices for %s are incomplete", rarity)
		}
		for _, value := range values {
			if value < 0 {
				return fmt.Errorf("CN buddy evolution price for %s is negative", rarity)
			}
		}
	}
	buddySeedIDs := make(map[int64]release.Buddy, len(master.BuddySeedTemplates))
	for _, buddy := range master.BuddySeedTemplates {
		definition, exists := buddyDefinitions[buddy.BuddyID]
		if !exists || buddy.UniqueID <= 0 || buddy.IsLock < 0 || buddy.IsLock > 1 {
			return fmt.Errorf("invalid CN buddy seed %d", buddy.UniqueID)
		}
		normalized, err := normalizeCNBuddy(buddy, definition, master.BuddyExperienceTables)
		if err != nil || normalized != buddy {
			return fmt.Errorf("CN buddy seed %d derived fields are invalid", buddy.UniqueID)
		}
		if _, duplicate := buddySeedIDs[buddy.UniqueID]; duplicate {
			return fmt.Errorf("duplicate CN buddy seed unique ID %d", buddy.UniqueID)
		}
		buddySeedIDs[buddy.UniqueID] = buddy
	}
	for arthurType := 1; arthurType <= 4; arthurType++ {
		uniqueIDs, exists := master.BuddySeedDecks[fmt.Sprint(arthurType)]
		if !exists || len(uniqueIDs) != 5 {
			return fmt.Errorf("CN buddy seed deck %d is incomplete", arthurType)
		}
		for _, uniqueID := range uniqueIDs {
			if uniqueID != 0 {
				if _, owned := buddySeedIDs[uniqueID]; !owned {
					return fmt.Errorf("CN buddy seed deck %d references unknown buddy %d", arthurType, uniqueID)
				}
			}
		}
	}
	for _, stack := range master.BuddySeedStackCards {
		template, exists := stackIDs[stack.CardID]
		if !exists || stack.Num <= 0 || stack.MaterialType < 9 || stack.MaterialType > 10 ||
			stack.AddExperience != template.AddExperience || stack.BaseAddPrice != template.BaseAddPrice {
			return fmt.Errorf("invalid CN buddy seed stack card %d", stack.CardID)
		}
	}
	return nil
}

func validCNCardProgressionSourceState(policy release.CardProgressionPolicy) bool {
	return len(policy.SourceState.ExperienceTables) > len("CONFIRMED:") &&
		policy.SourceState.ExperienceTables[:len("CONFIRMED:")] == "CONFIRMED:" &&
		len(policy.SourceState.ParameterFormula) > len("CONFIRMED:") &&
		policy.SourceState.ParameterFormula[:len("CONFIRMED:")] == "CONFIRMED:" &&
		len(policy.SourceState.FusionMaterialExperience) > len("INFERRED:") &&
		policy.SourceState.FusionMaterialExperience[:len("INFERRED:")] == "INFERRED:" &&
		len(policy.SourceState.FusionGold) > len("INFERRED:") &&
		policy.SourceState.FusionGold[:len("INFERRED:")] == "INFERRED:" &&
		strings.HasPrefix(policy.SourceState.FusionGoldConsumption, "CONFIRMED:") &&
		strings.HasPrefix(policy.SourceState.SellGoldConsumption, "CONFIRMED:") &&
		len(policy.SourceState.OrdinaryCardMaterialExperience) > len("PLACEHOLDER:") &&
		policy.SourceState.OrdinaryCardMaterialExperience[:len("PLACEHOLDER:")] == "PLACEHOLDER:" &&
		strings.HasPrefix(policy.SourceState.MaterialSelection, "CONFIRMED:") &&
		strings.HasPrefix(policy.SourceState.SkillLevelMaximum, "CONFIRMED:") &&
		strings.HasPrefix(policy.SourceState.FameInheritance, "CONFIRMED:") &&
		strings.HasPrefix(policy.SourceState.SuccessMultipliers, "INFERRED:") &&
		strings.HasPrefix(policy.SourceState.SuccessWeights, "PLACEHOLDER:")
}

func validCNSphereProgressionSourceState(policy release.SphereProgressionPolicy) bool {
	return strings.HasPrefix(policy.SourceState.ExperienceTables, "CONFIRMED:") &&
		strings.HasPrefix(policy.SourceState.MaterialBaseExperience, "INFERRED:") &&
		strings.HasPrefix(policy.SourceState.MaterialLevelBonus, "INFERRED:") &&
		strings.HasPrefix(policy.SourceState.FusionGold, "PLACEHOLDER:") &&
		strings.HasPrefix(policy.SourceState.EvolutionMaterialSelection, "CONFIRMED:") &&
		strings.HasPrefix(policy.SourceState.EvolutionLevelRequirement, "CONFIRMED:") &&
		strings.HasPrefix(policy.SourceState.EvolutionProgressionPreservation, "INFERRED:") &&
		strings.HasPrefix(policy.SourceState.AccountSeed, "PLACEHOLDER:")
}

func validCNBuddyProgressionSourceState(policy release.BuddyProgressionPolicy) bool {
	return strings.HasPrefix(policy.SourceState.ExperienceTables, "CONFIRMED:") &&
		strings.HasPrefix(policy.SourceState.MaterialExperienceConsumption, "CONFIRMED:") &&
		strings.HasPrefix(policy.SourceState.FusionGoldConsumption, "CONFIRMED:") &&
		strings.HasPrefix(policy.SourceState.OrdinaryBuddyMaterialExperience, "PLACEHOLDER:") &&
		strings.HasPrefix(policy.SourceState.AncientMelodyExperience, "INFERRED:") &&
		strings.HasPrefix(policy.SourceState.SuccessMultipliers, "INFERRED:") &&
		strings.HasPrefix(policy.SourceState.SuccessWeights, "PLACEHOLDER:") &&
		strings.HasPrefix(policy.SourceState.EvolutionMaterialSelection, "CONFIRMED:") &&
		strings.HasPrefix(policy.SourceState.EvolutionLevelRequirement, "CONFIRMED:") &&
		strings.HasPrefix(policy.SourceState.EvolutionProgressionPreservation, "INFERRED:") &&
		strings.HasPrefix(policy.SourceState.AccountSeed, "PLACEHOLDER:")
}

func validateCNCardFusionSuccessPolicy(policy release.CardProgressionPolicy) error {
	if len(policy.FusionSuccessTypes) != 3 {
		return errors.New("CN card fusion success policy must contain three result types")
	}
	for index, result := range policy.FusionSuccessTypes {
		if result.SuccessType != index || result.Weight <= 0 || result.ExperiencePermille < 1000 ||
			(index == 0 && result.ExperiencePermille != 1000) {
			return fmt.Errorf("CN card fusion success type %d is invalid", index)
		}
	}
	return nil
}

func applyCNCardRuntimeMaster(state *release.State, master cnCardRuntimeMaster) (bool, error) {
	state.DeckRankPolicy = master.DeckRankPolicy
	state.CardTemplates = append([]release.Card(nil), master.CardTemplates...)
	state.CardCategoryProfiles = append(
		[]release.CardCategoryProfile(nil), master.CardCategoryProfiles...,
	)
	state.CardGroupProfiles = cloneCNCardGroupProfiles(master.CardGroupProfiles)
	state.CardCollectionPages = make([][10]int, len(master.CardCollectionPages))
	for i, page := range master.CardCollectionPages {
		state.CardCollectionPages[i] = [10]int(page)
	}
	state.CardProgressionPolicy = master.CardProgressionPolicy
	state.CardExperienceTables = cloneCNCardExperienceTables(master.CardExperienceTables)
	state.SphereProgressionPolicy = master.SphereProgressionPolicy
	state.BuddyProgressionPolicy = master.BuddyProgressionPolicy
	state.CardDevelopmentPolicy = master.CardDevelopmentPolicy
	state.CardActions.EvolutionTransitions = append(
		[]release.EvolutionTransition(nil), master.EvolutionTransitions...,
	)
	state.SupportDeckSetCardNum = master.SupportDeckSetCardNum
	state.SupportDeckSlotUnlockRules = append(
		[]release.SupportDeckSlotUnlockRule(nil),
		master.SupportDeckSlotUnlockRules...,
	)
	if state.CardProgressionConfigVersion < 0 ||
		state.CardProgressionConfigVersion > master.CardProgressionConfigVersion {
		return false, fmt.Errorf(
			"CN save card progression config version %d is unsupported",
			state.CardProgressionConfigVersion,
		)
	}
	changed := false
	legacyCardProgression := state.CardProgressionConfigVersion < master.CardProgressionConfigVersion
	cardTemplates := make(map[int]release.Card, len(master.CardTemplates))
	for _, template := range master.CardTemplates {
		cardTemplates[template.CardID] = template
	}
	for _, inventory := range []*[]release.Card{&state.Cards, &state.ContainerCards} {
		for index, card := range *inventory {
			template, exists := cardTemplates[card.CardID]
			if !exists {
				return false, fmt.Errorf("CN save inventory references unknown card %d", card.CardID)
			}
			values := master.CardExperienceTables[template.ExperienceTableID]
			normalized, err := normalizeCNCard(
				card, template, values, master.CardProgressionPolicy, legacyCardProgression,
			)
			if err != nil {
				return false, err
			}
			if !reflect.DeepEqual(normalized, card) {
				if !legacyCardProgression {
					return false, fmt.Errorf("CN card %d differs from its progression contract", card.UniqueID)
				}
				(*inventory)[index] = normalized
				changed = true
			}
		}
	}
	if legacyCardProgression {
		state.CardProgressionConfigVersion = master.CardProgressionConfigVersion
		state.CardActions.FusionGoldPerCard = master.CardProgressionPolicy.FusionGoldPerMaterialPerBaseLevel
		changed = true
	}
	if state.CardActions.FusionGoldPerCard != master.CardProgressionPolicy.FusionGoldPerMaterialPerBaseLevel {
		return false, errors.New("CN save card fusion price differs from its progression contract")
	}
	if state.SupportDeckConfigVersion < 0 ||
		state.SupportDeckConfigVersion > master.SupportDeckConfigVersion {
		return false, fmt.Errorf(
			"CN save support-deck config version %d is unsupported",
			state.SupportDeckConfigVersion,
		)
	}
	if state.SupportDeckConfigVersion < master.SupportDeckConfigVersion {
		if len(state.SupportDeck.UnlockSlotNums) == 0 {
			state.SupportDeck.UnlockSlotNums = make([]int8, 4)
		}
		// Preserve any support cards already present in a legacy local save while
		// replacing the old erroneous "all ten slots unlocked" projection.
		for _, deck := range state.Decks {
			if deck.ArthurType < 1 || deck.ArthurType > 4 {
				continue
			}
			occupied := 0
			for index, uniqueID := range deck.SupportCardUniqueIDs {
				if uniqueID != 0 && index+1 > occupied {
					occupied = index + 1
				}
			}
			if occupied > master.SupportDeckSetCardNum {
				return false, fmt.Errorf(
					"CN legacy deck %d/%d uses support slot %d beyond the official active count",
					deck.ArthurType, deck.Index, occupied,
				)
			}
			if int(state.SupportDeck.UnlockSlotNums[deck.ArthurType-1]) < occupied {
				state.SupportDeck.UnlockSlotNums[deck.ArthurType-1] = int8(occupied)
			}
		}
		state.SupportDeckConfigVersion = master.SupportDeckConfigVersion
		changed = true
	}
	if len(state.SupportDeck.UnlockSlotNums) != 4 {
		return false, errors.New("CN save support-deck progression must contain four Arthur slots")
	}
	for index, count := range state.SupportDeck.UnlockSlotNums {
		if count < 0 || int(count) > master.SupportDeckSetCardNum {
			return false, fmt.Errorf("CN save support-deck slot count for Arthur %d is invalid", index+1)
		}
	}
	knownCardIDs := make(map[int]struct{}, len(master.CardTemplates))
	for _, card := range master.CardTemplates {
		knownCardIDs[card.CardID] = struct{}{}
	}
	for _, card := range master.StackCardTemplates {
		knownCardIDs[card.CardID] = struct{}{}
	}
	collected := make(map[int]struct{}, len(state.SupportDeck.CardCollectionIDs)+len(state.Cards)+len(state.ContainerCards))
	for _, cardID := range state.SupportDeck.CardCollectionIDs {
		if _, exists := knownCardIDs[cardID]; !exists {
			return false, fmt.Errorf("CN save card collection references unknown card %d", cardID)
		}
		collected[cardID] = struct{}{}
	}
	for _, inventory := range [][]release.Card{state.Cards, state.ContainerCards} {
		for _, card := range inventory {
			if _, exists := knownCardIDs[card.CardID]; !exists {
				return false, fmt.Errorf("CN save inventory references unknown card %d", card.CardID)
			}
			collected[card.CardID] = struct{}{}
		}
	}
	for _, card := range state.StackCards {
		if card.Num > 0 {
			collected[card.CardID] = struct{}{}
		}
	}
	normalizedCollection := make([]int, 0, len(collected))
	for cardID := range collected {
		normalizedCollection = append(normalizedCollection, cardID)
	}
	sort.Ints(normalizedCollection)
	if !equalIntSlices(state.SupportDeck.CardCollectionIDs, normalizedCollection) {
		state.SupportDeck.CardCollectionIDs = normalizedCollection
		changed = true
	}
	stackTemplates := make(map[int]release.CardStack, len(master.StackCardTemplates))
	if normalizeCNCollectionLoveHistory(state) {
		changed = true
	}
	for _, template := range master.StackCardTemplates {
		stackTemplates[template.CardID] = template
	}
	state.StackCardTemplates = append([]release.CardStack(nil), master.StackCardTemplates...)
	for index, inventory := range state.StackCards {
		template, exists := stackTemplates[inventory.CardID]
		if !exists {
			return false, fmt.Errorf("CN save references unknown stack card %d", inventory.CardID)
		}
		if inventory.Num < 0 {
			return false, fmt.Errorf("CN save stack card %d has negative inventory", inventory.CardID)
		}
		template.Num = inventory.Num
		state.StackCards[index] = template
	}
	state.SphereDefinitions = append([]release.SphereDefinition(nil), master.SphereDefinitions...)
	state.SphereExperienceTables = cloneCNSphereExperienceTables(master.SphereExperienceTables)
	state.SphereEvolutionPrices = cloneCNSphereEvolutionPrices(master.SphereEvolutionPrices)
	cleanOnboardingSpheres := state.Onboarding.ConfigVersion == cnOnboardingConfigVersion &&
		state.Onboarding.Step < cnOnboardingStepCount
	if cleanOnboardingSpheres {
		if len(state.Spheres) != 0 {
			state.Spheres = []release.Sphere{}
			changed = true
		}
		for index := range state.Decks {
			if len(state.Decks[index].SphereUniqueIDs) != 3 ||
				!allZeroInt64(state.Decks[index].SphereUniqueIDs) {
				state.Decks[index].SphereUniqueIDs = make([]int64, 3)
				changed = true
			}
		}
	}
	if state.SphereConfigVersion < 0 || state.SphereConfigVersion > master.SphereConfigVersion {
		return false, fmt.Errorf("CN save sphere config version %d is unsupported", state.SphereConfigVersion)
	}
	legacySphereProgression := state.SphereConfigVersion < master.SphereConfigVersion
	if legacySphereProgression {
		if len(state.Spheres) == 0 && !cleanOnboardingSpheres {
			state.Spheres = append([]release.Sphere{}, master.SphereSeedTemplates...)
			for index := range state.Decks {
				deck := &state.Decks[index]
				if deck.ArthurType < 1 || deck.ArthurType > 4 || !allZeroInt64(deck.SphereUniqueIDs) {
					continue
				}
				deck.SphereUniqueIDs = append([]int64(nil), master.SphereSeedDecks[fmt.Sprint(deck.ArthurType)]...)
			}
		}
		state.SphereConfigVersion = master.SphereConfigVersion
		changed = true
	}
	definitions := make(map[int]release.SphereDefinition, len(master.SphereDefinitions))
	for _, definition := range master.SphereDefinitions {
		definitions[definition.SphereID] = definition
	}
	uniqueIDs := make(map[int64]struct{}, len(state.Spheres))
	for index, sphere := range state.Spheres {
		definition, exists := definitions[sphere.SphereID]
		if !exists || sphere.UniqueID <= 0 || sphere.IsLock < 0 || sphere.IsLock > 1 {
			return false, fmt.Errorf("CN save references invalid sphere %d", sphere.UniqueID)
		}
		if _, duplicate := uniqueIDs[sphere.UniqueID]; duplicate {
			return false, fmt.Errorf("CN save repeats sphere unique ID %d", sphere.UniqueID)
		}
		uniqueIDs[sphere.UniqueID] = struct{}{}
		if legacySphereProgression {
			migrated, err := migrateLegacyCNSphereExperience(
				sphere, definition, master.SphereExperienceTables,
			)
			if err != nil {
				return false, err
			}
			sphere = migrated
		}
		normalized, err := normalizeCNSphere(
			sphere, definition, master.SphereExperienceTables, master.SphereProgressionPolicy,
		)
		if err != nil {
			return false, err
		}
		if normalized != sphere {
			if !legacySphereProgression {
				return false, fmt.Errorf("CN sphere %d differs from its progression contract", sphere.UniqueID)
			}
			state.Spheres[index] = normalized
			changed = true
		} else if legacySphereProgression && state.Spheres[index] != normalized {
			state.Spheres[index] = normalized
			changed = true
		}
	}
	state.BuddyDefinitions = append([]release.BuddyDefinition(nil), master.BuddyDefinitions...)
	state.BuddyExperienceTables = cloneCNBuddyExperienceTables(master.BuddyExperienceTables)
	state.BuddyEvolutionPrices = cloneCNBuddyEvolutionPrices(master.BuddyEvolutionPrices)
	if state.BuddyConfigVersion < 0 || state.BuddyConfigVersion > master.BuddyConfigVersion {
		return false, fmt.Errorf("CN save buddy config version %d is unsupported", state.BuddyConfigVersion)
	}
	if state.BuddyConfigVersion < master.BuddyConfigVersion {
		if state.BuddyConfigVersion == 0 {
			if state.Onboarding.ConfigVersion != 0 {
				state.Buddies = []release.Buddy{}
				for index := range state.Decks {
					state.Decks[index].BuddyUniqueIDs = make([]int64, 5)
				}
			} else {
				state.Buddies = append([]release.Buddy(nil), master.BuddySeedTemplates...)
				for index := range state.Decks {
					deck := &state.Decks[index]
					if deck.ArthurType < 1 || deck.ArthurType > 4 {
						continue
					}
					deck.BuddyUniqueIDs = append([]int64(nil), master.BuddySeedDecks[fmt.Sprint(deck.ArthurType)]...)
				}
				ownedStacks := make(map[int]struct{}, len(state.StackCards))
				for _, stack := range state.StackCards {
					ownedStacks[stack.CardID] = struct{}{}
				}
				for _, stack := range master.BuddySeedStackCards {
					if _, exists := ownedStacks[stack.CardID]; !exists {
						state.StackCards = append(state.StackCards, stack)
					}
				}
			}
		}
		state.BuddyConfigVersion = master.BuddyConfigVersion
		state.Buddy = release.Buddy{}
		changed = true
	}
	if len(state.Buddies) == 0 && state.Onboarding.ConfigVersion == 0 {
		return false, errors.New("CN QA save owns no buddies")
	}
	if len(state.Buddies) > state.User.BuddyMax {
		return false, fmt.Errorf("CN save owns %d buddies but capacity is %d", len(state.Buddies), state.User.BuddyMax)
	}
	buddyDefinitions := make(map[int]release.BuddyDefinition, len(master.BuddyDefinitions))
	for _, definition := range master.BuddyDefinitions {
		buddyDefinitions[definition.BuddyID] = definition
	}
	buddyUniqueIDs := make(map[int64]struct{}, len(state.Buddies))
	for index, buddy := range state.Buddies {
		definition, exists := buddyDefinitions[buddy.BuddyID]
		if !exists || buddy.UniqueID <= 0 || buddy.IsLock < 0 || buddy.IsLock > 1 {
			return false, fmt.Errorf("CN save references invalid buddy %d", buddy.UniqueID)
		}
		if _, duplicate := buddyUniqueIDs[buddy.UniqueID]; duplicate {
			return false, fmt.Errorf("CN save repeats buddy unique ID %d", buddy.UniqueID)
		}
		buddyUniqueIDs[buddy.UniqueID] = struct{}{}
		normalized, err := normalizeCNBuddy(buddy, definition, master.BuddyExperienceTables)
		if err != nil {
			return false, err
		}
		if normalized != buddy {
			state.Buddies[index] = normalized
			changed = true
		}
	}
	for _, deck := range state.Decks {
		for _, uniqueID := range deck.BuddyUniqueIDs {
			if uniqueID != 0 {
				if _, exists := buddyUniqueIDs[uniqueID]; !exists {
					return false, fmt.Errorf("CN deck references unknown buddy unique ID %d", uniqueID)
				}
			}
		}
	}
	return changed, nil
}

func validateCNCardGroupProfiles(master cnCardRuntimeMaster) error {
	policy := master.CardGroupPolicy
	if policy.ConfigVersion != 2 || policy.ClaimsOriginalServiceTopology ||
		!strings.HasPrefix(policy.ClientContract, "CONFIRMED:") ||
		!strings.HasPrefix(policy.GroupMembership, "CONFIRMED:") ||
		!strings.HasPrefix(policy.DisplayNames, "INFERRED:") ||
		!strings.HasPrefix(policy.CategoryProjection, "PLACEHOLDER:") ||
		!strings.HasPrefix(policy.DeckLimitBossID, "CONFIRMED:") {
		return errors.New("CN card group policy is invalid")
	}
	category := master.CardCategoryProfiles[0]
	if category.CategoryID != 60200001 || category.Name == "" || category.Order != 2 ||
		category.ViewType != 0 || !strings.HasPrefix(category.Evidence, "PLACEHOLDER:") {
		return errors.New("CN card category profile is invalid")
	}
	expectedGroupIDs := []int{20020001, 30020001, 30020002, 30020003, 30020005, 90020001}
	knownCards := make(map[int]struct{}, len(master.CardTemplates))
	for _, card := range master.CardTemplates {
		knownCards[card.CardID] = struct{}{}
	}
	for index, group := range master.CardGroupProfiles {
		if group.GroupID != expectedGroupIDs[index] || group.CategoryID != category.CategoryID ||
			group.Name == "" || group.Order != 10+index || group.DeckLimitBossID != 0 ||
			len(group.MemberCardIDs) == 0 || !strings.HasPrefix(group.NameEvidence, "INFERRED:") {
			return fmt.Errorf("CN card group profile %d is invalid", group.GroupID)
		}
		previous := 0
		for _, cardID := range group.MemberCardIDs {
			if cardID <= previous {
				return fmt.Errorf("CN card group %d members are not unique and sorted", group.GroupID)
			}
			if _, exists := knownCards[cardID]; !exists {
				return fmt.Errorf("CN card group %d references unknown card %d", group.GroupID, cardID)
			}
			previous = cardID
		}
	}
	return nil
}

func cloneCNCardGroupProfiles(source []release.CardGroupProfile) []release.CardGroupProfile {
	result := make([]release.CardGroupProfile, len(source))
	for index, group := range source {
		result[index] = group
		result[index].MemberCardIDs = append([]int(nil), group.MemberCardIDs...)
	}
	return result
}

func equalIntSlices(left []int, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func normalizeCNCard(
	card release.Card,
	template release.Card,
	experienceTable []int,
	policy release.CardProgressionPolicy,
	migrateLegacy bool,
) (release.Card, error) {
	if card.UniqueID < 0 || card.Level < 1 || card.Level > template.LevelMax ||
		card.Experience < 0 || card.Love < 0 || card.Love > template.LoveMax ||
		card.Fame < 0 || card.Fame > template.FameMax || card.IsLock < 0 || card.IsLock > 1 {
		return release.Card{}, fmt.Errorf("CN card %d mutable state is invalid", card.UniqueID)
	}
	experience := card.Experience
	if migrateLegacy && experience == 0 && card.Level > 1 {
		var err error
		experience, err = release.CardExperienceAtLevelStart(card.Level, template.LevelMax, experienceTable)
		if err != nil {
			return release.Card{}, fmt.Errorf("normalize CN legacy card %d: %w", card.UniqueID, err)
		}
	}
	experienceState, err := release.NormalizeCardExperience(template.LevelMax, experience, experienceTable)
	if err != nil {
		return release.Card{}, fmt.Errorf("normalize CN card %d experience: %w", card.UniqueID, err)
	}
	parameters, err := policy.ParametersAt(template, experienceState.Level, card.Love, card.Fame)
	if err != nil {
		return release.Card{}, fmt.Errorf("normalize CN card %d parameters: %w", card.UniqueID, err)
	}
	normalized := template
	normalized.UniqueID = card.UniqueID
	normalized.Level = experienceState.Level
	normalized.Experience = experienceState.Experience
	normalized.NowLevelExperience = experienceState.NowLevelExperience
	normalized.NextLevelExperience = experienceState.NextLevelExperience
	normalized.Love = card.Love
	normalized.SkillLevels = append([]int16(nil), card.SkillLevels...)
	if len(normalized.SkillLevels) == 0 {
		return release.Card{}, fmt.Errorf("CN card %d has no skill levels", card.UniqueID)
	}
	normalized.HP = parameters.HP
	normalized.Attack = parameters.Attack
	normalized.Magic = parameters.Magic
	normalized.Mind = parameters.Mind
	fusionGold, err := policy.FusionGoldPerMaterial(experienceState.Level)
	if err != nil {
		return release.Card{}, fmt.Errorf("normalize CN card %d fusion gold: %w", card.UniqueID, err)
	}
	normalized.BaseAddPrice = fusionGold
	normalized.IsLock = card.IsLock
	normalized.Fame = card.Fame
	return normalized, nil
}

func cloneCNCardExperienceTables(source map[int][]int) map[int][]int {
	result := make(map[int][]int, len(source))
	for tableID, values := range source {
		result[tableID] = append([]int(nil), values...)
	}
	return result
}

func normalizeCNSphere(
	sphere release.Sphere,
	definition release.SphereDefinition,
	tables map[int][]int,
	policy release.SphereProgressionPolicy,
) (release.Sphere, error) {
	values, exists := tables[definition.ExperienceTableID]
	if !exists && definition.MaxLevel > 1 {
		return release.Sphere{}, fmt.Errorf("CN sphere %d experience table is unavailable", sphere.UniqueID)
	}
	progression, err := release.NormalizeSphereExperience(definition.MaxLevel, sphere.Experience, values)
	if err != nil || progression.Level != sphere.Level {
		return release.Sphere{}, fmt.Errorf("CN sphere %d experience does not match level %d", sphere.UniqueID, sphere.Level)
	}
	sphere.Experience = progression.Experience
	sphere.NowLevelExperience = progression.NowLevelExperience
	sphere.NextLevelExperience = progression.NextLevelExperience
	addExperience, err := release.SphereMaterialExperience(
		definition.MaterialAddExperience,
		sphere.Level,
		policy.MaterialLevelBonusPermillePerLevel,
	)
	if err != nil {
		return release.Sphere{}, fmt.Errorf("CN sphere %d material experience: %w", sphere.UniqueID, err)
	}
	sphere.AddExperience = addExperience
	sphere.BaseAddPrice = definition.FusionBaseAddPrice
	return sphere, nil
}

func migrateLegacyCNSphereExperience(
	sphere release.Sphere,
	definition release.SphereDefinition,
	tables map[int][]int,
) (release.Sphere, error) {
	values, exists := tables[definition.ExperienceTableID]
	if !exists && definition.MaxLevel > 1 {
		return release.Sphere{}, fmt.Errorf("CN sphere %d experience table is unavailable", sphere.UniqueID)
	}
	if sphere.Level < 1 || sphere.Level > definition.MaxLevel || sphere.Experience < 0 {
		return release.Sphere{}, fmt.Errorf("CN legacy sphere %d level or experience is invalid", sphere.UniqueID)
	}
	start, err := release.SphereExperienceAtLevelStart(sphere.Level, definition.MaxLevel, values)
	if err != nil {
		return release.Sphere{}, fmt.Errorf("CN legacy sphere %d: %w", sphere.UniqueID, err)
	}
	within := sphere.Experience
	if sphere.Level > 1 {
		within -= values[sphere.Level-2]
	}
	if within < 0 {
		within = 0
	}
	if sphere.Level < definition.MaxLevel && within >= values[sphere.Level-1] {
		within = values[sphere.Level-1] - 1
	}
	if sphere.Level == definition.MaxLevel {
		within = 0
	}
	if start > math.MaxInt-within {
		return release.Sphere{}, fmt.Errorf("CN legacy sphere %d experience overflows", sphere.UniqueID)
	}
	sphere.Experience = start + within
	return sphere, nil
}

func cloneCNSphereExperienceTables(source map[int][]int) map[int][]int {
	result := make(map[int][]int, len(source))
	for tableID, values := range source {
		result[tableID] = append([]int(nil), values...)
	}
	return result
}

func cloneCNSphereEvolutionPrices(source map[string][]int) map[string][]int {
	result := make(map[string][]int, len(source))
	for rarity, values := range source {
		result[rarity] = append([]int(nil), values...)
	}
	return result
}

func normalizeCNBuddy(buddy release.Buddy, definition release.BuddyDefinition, tables map[int][]int) (release.Buddy, error) {
	if buddy.Level < 1 || buddy.Level > definition.MaxLevel || buddy.Experience < 0 {
		return release.Buddy{}, fmt.Errorf("CN buddy %d level or experience is invalid", buddy.UniqueID)
	}
	values, exists := tables[definition.ExperienceTableID]
	if !exists || len(values) < definition.MaxLevel-1 {
		return release.Buddy{}, fmt.Errorf("CN buddy %d experience table is unavailable", buddy.UniqueID)
	}
	spent := 0
	for level := 1; level < buddy.Level; level++ {
		spent += values[level-1]
	}
	if buddy.Experience < spent {
		return release.Buddy{}, fmt.Errorf("CN buddy %d experience does not match level %d", buddy.UniqueID, buddy.Level)
	}
	nowExperience, nextExperience := 0, 0
	if buddy.Level < definition.MaxLevel {
		required := values[buddy.Level-1]
		nowExperience = buddy.Experience - spent
		if nowExperience < 0 || nowExperience >= required {
			return release.Buddy{}, fmt.Errorf("CN buddy %d experience does not match level %d", buddy.UniqueID, buddy.Level)
		}
		nextExperience = required - nowExperience
	} else if buddy.Experience != spent {
		return release.Buddy{}, fmt.Errorf("CN buddy %d maximum-level experience is invalid", buddy.UniqueID)
	}
	buddy.NowLevelExperience = nowExperience
	buddy.NextLevelExperience = nextExperience
	buddy.AddExperience = definition.MaterialAddExperience
	buddy.BaseAddPrice = definition.FusionBaseAddPrice
	return buddy, nil
}

func cloneCNBuddyExperienceTables(source map[int][]int) map[int][]int {
	result := make(map[int][]int, len(source))
	for tableID, values := range source {
		result[tableID] = append([]int(nil), values...)
	}
	return result
}

func cloneCNBuddyEvolutionPrices(source map[string][]int) map[string][]int {
	result := make(map[string][]int, len(source))
	for rarity, values := range source {
		result[rarity] = append([]int(nil), values...)
	}
	return result
}

func allZeroInt64(values []int64) bool {
	for _, value := range values {
		if value != 0 {
			return false
		}
	}
	return true
}
