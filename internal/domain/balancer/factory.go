package balancer

type Factory struct {
	healthChecker HealthChecker
}

func NewBalancerFactory(healthChecker HealthChecker) *Factory {
	return &Factory{
		healthChecker: healthChecker,
	}
}

func (f *Factory) CreateBalancer(algorithm string) (Balancer, error) {
	switch algorithm {
	case "round-robin":
		return NewRoundRobinBalancer(f.healthChecker), nil
	case "random":
		return NewRandomBalancer(f.healthChecker), nil
	case "least-connections":
		return NewLeastConnectionsBalancer(f.healthChecker), nil
	default:
		return NewRoundRobinBalancer(f.healthChecker), nil
	}
}
