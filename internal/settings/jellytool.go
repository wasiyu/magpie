package settings

// jellytoolSettings admits display preferences and the existing Agent model
// selections only. A copied settings file cannot enable retired subsystems.
func jellytoolSettings(s Settings) Settings {
	out := Settings{Lang: s.Lang, Theme: s.Theme, TextSize: s.TextSize, Colorblind: s.Colorblind, ReduceMotion: s.ReduceMotion,
		AgentOrder: s.AgentOrder, AgentsHidden: s.AgentsHidden, AgentsShown: s.AgentsShown,
		Visible: s.Visible, HiddenModels: s.HiddenModels, PickedModels: s.PickedModels, OwnModelsHidden: s.OwnModelsHidden, OrderedModels: s.OrderedModels}
	out.Port = 3435
	return out
}
