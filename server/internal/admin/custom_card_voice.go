package admin

import (
	"fmt"
	"kairisei.local/server/internal/criaudio"
	"strconv"
)

// GetSkillInvokeVoiceID reads voices[7]/[8], i.e. card.csv[89]/[90].
// The existing voice ID identifies the audio cue; switching it reuses the
// installed client audio rather than changing the subtitle or voice credit.
type customCardVoiceSource struct {
	CardID int    `json:"card_id"`
	Kind   string `json:"kind"`
}

type customCardVoiceIDs struct {
	Attack  int `json:"attack"`
	Support int `json:"support"`
}

func customVoiceColumn(kind string) int {
	switch kind {
	case "attack":
		return 89
	case "support":
		return 90
	default:
		return -1
	}
}

func customCardVoiceSourceIDs(c customCard) []int {
	ids := []int{}
	for _, source := range []*customCardVoiceSource{c.AttackVoice, c.SupportVoice} {
		if source != nil {
			ids = append(ids, source.CardID)
		}
	}
	return ids
}

func customCardVoiceReferences(cards []customCard) []customCard {
	refs := append([]customCard(nil), cards...)
	for _, c := range cards {
		for _, id := range customCardVoiceSourceIDs(c) {
			refs = append(refs, customCard{TemplateID: id})
		}
	}
	return refs
}

func customCardTemplateVoices(s customCardSources, cards []customCard) map[int]customCardVoiceIDs {
	voices := map[int]customCardVoiceIDs{}
	for _, c := range customCardVoiceReferences(cards) {
		row := s.Cards[c.TemplateID]
		voices[c.TemplateID] = customCardVoiceIDs{customRowInt(row, 89), customRowInt(row, 90)}
	}
	return voices
}

func customCardVoices(c customCard, s customCardSources, ready func(int) bool) (map[int]string, error) {
	voices := map[int]string{}
	for i, source := range []*customCardVoiceSource{c.AttackVoice, c.SupportVoice} {
		raw := [][]byte{c.AttackVoiceAudio, c.SupportVoiceAudio}[i]
		if len(raw) > 0 {
			if source != nil {
				return nil, fmt.Errorf("同一类出牌语音不能同时选择来源和上传音频")
			}
			if c.ID < customCardFirstID || c.ID > customCardLastID || len(s.Cards[c.TemplateID]) <= 89+i {
				return nil, fmt.Errorf("自制语音卡牌编号或模板无效")
			}
			if _, err := criaudio.ParseWAV(raw); err != nil {
				return nil, err
			}
			id := customVoiceID(c, i)
			for cardID, row := range s.Cards {
				if cardID != c.ID && (customRowInt(row, 89) == id || customRowInt(row, 90) == id) {
					return nil, fmt.Errorf("自制语音编号与现有资源冲突")
				}
			}
			voices[89+i] = strconv.Itoa(id)
			continue
		}
		if source == nil {
			continue
		}
		if len(s.Cards[c.TemplateID]) <= 89+i {
			return nil, fmt.Errorf("模板卡牌表缺少出牌语音字段")
		}
		column := customVoiceColumn(source.Kind)
		if column < 0 {
			return nil, fmt.Errorf("出牌语音来源类型须为攻击类或支援类")
		}
		if ready != nil && !ready(source.CardID) {
			return nil, fmt.Errorf("出牌语音来源卡牌资源不完整")
		}
		if _, err := s.template(source.CardID); err != nil {
			return nil, fmt.Errorf("出牌语音来源无效：%w", err)
		}
		row := s.Cards[source.CardID]
		id := customRowInt(row, column)
		if id <= 0 || id > 2147483647 {
			return nil, fmt.Errorf("来源卡牌没有可用的这类出牌语音，请选择另一类语音或其他卡牌")
		}
		voices[89+i] = strconv.Itoa(id)
	}
	return voices, nil
}
