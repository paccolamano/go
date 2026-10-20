// Package password hashes passwords with argon2id and checks them, also
// against legacy bcrypt hashes.
//
// Hashes use the PHC string format,
// $argon2id$v=19$m=<KiB>,t=<iterations>,p=<parallelism>$<salt>$<key>. Every
// check costs one argon2id computation and the memory of its parameters, so
// bound the concurrency of the routes that check passwords.
package password

import (
	"fmt"

	"github.com/paccolamano/go/auth"
)

// Params are argon2id parameters.
type Params struct {
	// Memory is the memory cost in KiB.
	Memory uint32
	// Iterations is the time cost.
	Iterations uint32
	// Parallelism is the number of lanes.
	Parallelism uint8
}

// DefaultParams returns the default parameters, the first configuration of
// the OWASP Password Storage Cheat Sheet: 19 MiB, 2 iterations, parallelism 1.
func DefaultParams() Params {
	return Params{Memory: 19456, Iterations: 2, Parallelism: 1}
}

const (
	saltLen      = 16
	keyLen       = 32
	argonVersion = 19

	// Floor for configured parameters: every OWASP configuration meets it.
	minMemory      = 7168
	minMemoryTimes = 35840

	// Bounds for stored hashes.
	maxStoredMemory     = 2097152
	maxStoredIterations = 100
	maxParallelism      = 64
	minSalt, maxSalt    = 8, 64
	minKey, maxKey      = 16, 64
)

// validate checks configured parameters against the floor.
func (p Params) validate() error {
	switch {
	case p.Parallelism < 1 || p.Parallelism > maxParallelism:
		return fmt.Errorf("%w: password.NewChecker: WithParams: parallelism must be between 1 and %d", auth.ErrInvalidConfig, maxParallelism)
	case p.Iterations < 1 || p.Iterations > maxStoredIterations:
		return fmt.Errorf("%w: password.NewChecker: WithParams: iterations must be between 1 and %d", auth.ErrInvalidConfig, maxStoredIterations)
	case p.Memory < minMemory || p.Memory > maxStoredMemory:
		return fmt.Errorf("%w: password.NewChecker: WithParams: memory must be between %d and %d KiB", auth.ErrInvalidConfig, minMemory, maxStoredMemory)
	case uint64(p.Memory)*uint64(p.Iterations) < minMemoryTimes:
		return fmt.Errorf("%w: password.NewChecker: WithParams: memory × iterations must be at least %d KiB", auth.ErrInvalidConfig, minMemoryTimes)
	}
	return nil
}

// stored reports whether parameters read from a stored hash are within bounds.
func (p Params) stored() bool {
	return p.Memory >= 1 && p.Memory <= maxStoredMemory &&
		p.Iterations >= 1 && p.Iterations <= maxStoredIterations &&
		p.Parallelism >= 1 && p.Parallelism <= maxParallelism
}
