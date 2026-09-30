package locks

import "github.com/3to1go/shared/keylock"

type NamespaceLockManager = keylock.Manager

func NewNamespaceLockManager() *NamespaceLockManager { return keylock.New() }
