package game

type EvolutionPath struct {
	FromCardID int `json:"from_cardid"`
	ToCardID   int `json:"to_cardid"`
}

// Immutable, globally configured edges; never stored in player snapshots.
type EvolutionRestrictions struct{ blocked map[EvolutionPath]struct{} }

func NewEvolutionRestrictions(paths []EvolutionPath) *EvolutionRestrictions {
	p := &EvolutionRestrictions{blocked: make(map[EvolutionPath]struct{}, len(paths))}
	for _, edge := range paths {
		p.blocked[edge] = struct{}{}
	}
	return p
}

type EvolutionConfigurator interface {
	ApplyEvolutionRestrictions(*EvolutionRestrictions)
}

func (s *Account) ApplyEvolutionRestrictions(policy *EvolutionRestrictions) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evolutionRestrictions = policy
}

var ErrEvolutionClosed = &BusinessError{-1, "该进化路线尚未开放，请等待服务器开放后再尝试。"}
