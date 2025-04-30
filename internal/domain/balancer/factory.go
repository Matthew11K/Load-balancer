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
	switch AlgorithmType(algorithm) {
	case RoundRobin:
		return NewRoundRobinBalancer(f.healthChecker), nil
	case Random:
		return NewRandomBalancer(f.healthChecker), nil
	case LeastConnections:
		return NewLeastConnectionsBalancer(f.healthChecker), nil
	default:
		return NewRoundRobinBalancer(f.healthChecker), nil
	}
}
