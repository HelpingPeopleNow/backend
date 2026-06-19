package core

type Money struct {
	amount float64
}

func NewMoney(amount float64) Money {
	return Money{amount: amount}
}

func (m Money) Amount() float64 {
	return m.amount
}

func (m Money) IsZero() bool {
	return m.amount <= 0
}

func (m Money) PerHour() string {
	if m.IsZero() {
		return "—"
	}
	return fmtMoney(m.amount) + "/hr"
}

func (m Money) String() string {
	if m.IsZero() {
		return "—"
	}
	return fmtMoney(m.amount)
}
