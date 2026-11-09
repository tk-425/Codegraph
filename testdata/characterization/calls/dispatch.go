package fixture

type Runner interface{ Run() }

type Job struct{}

func (Job) Run() {}

func Dispatch(r Runner) {
	r.Run()
}
