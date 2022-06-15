package trylock


type TryLock struct {
	ch chan struct{}
}

func Build()*TryLock  {
	return &TryLock{
		ch: make(chan struct{},1),
	}
}

func (tl *TryLock)TryLock()bool  {
	select {
	case tl.ch<- struct{}{}:
		return true
	default:
		return false
	}
}

func (tl *TryLock)TryUnLock() bool {
	select {
	case _ = <-tl.ch :
		return true
	default:
		return false
	}
}