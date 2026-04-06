package lock

import "sync"

const shardCount = 256

// ActivationLock is a bounded-memory sharded lock keyed by license ID.
// Collisions are acceptable and only reduce parallelism.
type ActivationLock struct {
	shards [shardCount]sync.Mutex
}

func NewActivationLock() *ActivationLock {
	return &ActivationLock{}
}

func (al *ActivationLock) shard(licenseID string) int {
	var sum uint32
	for i := 0; i < len(licenseID); i++ {
		sum = sum*33 + uint32(licenseID[i])
	}
	return int(sum % shardCount)
}

func (al *ActivationLock) Lock(licenseID string) {
	al.shards[al.shard(licenseID)].Lock()
}

func (al *ActivationLock) Unlock(licenseID string) {
	al.shards[al.shard(licenseID)].Unlock()
}
