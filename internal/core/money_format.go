package core

import (
	"fmt"
	"math"
)

func fmtMoney(amount float64) string {
	if amount == math.Trunc(amount) && !math.IsInf(amount, 0) && !math.IsNaN(amount) {
		return fmt.Sprintf("€%.0f", amount)
	}
	return fmt.Sprintf("€%.2f", amount)
}
