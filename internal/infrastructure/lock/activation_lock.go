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

func (al *ActivationLock) shard(licenseID int) int {
	if licenseID < 0 {
		licenseID = -licenseID
	}
	return licenseID % shardCount
}

func (al *ActivationLock) Lock(licenseID int) {
	al.shards[al.shard(licenseID)].Lock()
}

func (al *ActivationLock) Unlock(licenseID int) {
	al.shards[al.shard(licenseID)].Unlock()
}
