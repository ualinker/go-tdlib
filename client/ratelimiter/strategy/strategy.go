package strategy

type Strategy interface {
	CurrentRate() float64
	Wait()
	FloodWaitEvent(int)
}
