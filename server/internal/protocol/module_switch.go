package protocol

// ModuleSwitch is the CN 6.0.2 MODULE_SWITCH bit position contract. Values come
// from Assembly-CSharp's enum; they are not server feature IDs or ordinal iota.
type ModuleSwitch int64

const (
	ModuleEverydayTask    ModuleSwitch = 1 << 2
	ModuleActivity        ModuleSwitch = 1 << 3
	ModuleRecommendDeck   ModuleSwitch = 1 << 4
	ModuleStrategyButton  ModuleSwitch = 1 << 22
	ModulePVP             ModuleSwitch = 1 << 28
	ModuleFailureAdvise   ModuleSwitch = 1 << 29
	ModuleCopyCard        ModuleSwitch = 1 << 31
	ModuleChangeCloth     ModuleSwitch = 1 << 34
	ModuleChangeModel     ModuleSwitch = 1 << 35
	ModuleReturnToDungeon ModuleSwitch = 1 << 37
)
