package cache

import "fmt"

func NewPolicy(name string, maxSize int64) (CachePolicy, error) {
	switch name {
	case "sieve":
		return NewSieveCache(maxSize), nil
	case "w-tinylfu":
		return NewWTinyLFUCache(maxSize), nil
	default:
		return nil, fmt.Errorf("unknown cache policy: %q", name)
	}
}
