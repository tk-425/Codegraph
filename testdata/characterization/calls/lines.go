package fixture

func fact(n int) int { if n < 2 { return 1 }; return n * fact(n-1) }

func target() {}

func caller() { target(); target() }
