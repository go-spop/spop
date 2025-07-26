package action

type Actions []Action

func (actions *Actions) SetVar(scope Scope, name string, value any) {
	*actions = append(*actions, NewSetVar(scope, name, value))
}

func (actions *Actions) UnsetVar(scope Scope, name string) {
	*actions = append(*actions, NewUnsetVar(scope, name))
}

func (actions *Actions) Reset() {
	*actions = (*actions)[:0]
}
