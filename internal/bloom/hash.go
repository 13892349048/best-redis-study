package bloom

import (
	"crypto/md5"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"hash/crc64"
	"hash/fnv"
)

// HashFunction 哈希函数类型
type HashFunction func(data []byte) uint64

// 预定义的哈希函数
var (
	hashFunctions = []HashFunction{
		hashFNV64,
		hashCRC32,
		hashCRC64,
		hashMD5,
		hashSHA1,
		hashDJB2,
		hashSDBM,
		hashJSHash,
	}
)

// GetHashFunctions 获取指定数量的哈希函数
func GetHashFunctions(count uint32) []HashFunction {
	if count == 0 {
		return nil
	}

	// 如果需要的哈希函数数量超过预定义数量，使用组合哈希
	if count > uint32(len(hashFunctions)) {
		return generateCombinedHashFunctions(count)
	}

	return hashFunctions[:count]
}

// hashFNV64 FNV-1a 64位哈希
func hashFNV64(data []byte) uint64 {
	h := fnv.New64a()
	h.Write(data)
	return h.Sum64()
}

// hashCRC32 CRC32哈希
func hashCRC32(data []byte) uint64 {
	return uint64(crc32.ChecksumIEEE(data))
}

// hashCRC64 CRC64哈希
func hashCRC64(data []byte) uint64 {
	return crc64.Checksum(data, crc64.MakeTable(crc64.ECMA))
}

// hashMD5 MD5哈希（取前8字节）
func hashMD5(data []byte) uint64 {
	h := md5.New()
	h.Write(data)
	sum := h.Sum(nil)
	return binary.BigEndian.Uint64(sum[:8])
}

// hashSHA1 SHA1哈希（取前8字节）
func hashSHA1(data []byte) uint64 {
	h := sha1.New()
	h.Write(data)
	sum := h.Sum(nil)
	return binary.BigEndian.Uint64(sum[:8])
}

// hashDJB2 DJB2哈希算法
func hashDJB2(data []byte) uint64 {
	var hash uint64 = 5381
	for _, b := range data {
		hash = ((hash << 5) + hash) + uint64(b) // hash * 33 + c
	}
	return hash
}

// hashSDBM SDBM哈希算法
func hashSDBM(data []byte) uint64 {
	var hash uint64
	for _, b := range data {
		hash = uint64(b) + (hash << 6) + (hash << 16) - hash
	}
	return hash
}

// hashJSHash JS Hash算法
func hashJSHash(data []byte) uint64 {
	var hash uint64 = 1315423911
	for _, b := range data {
		hash ^= ((hash << 5) + uint64(b) + (hash >> 2))
	}
	return hash
}

// generateCombinedHashFunctions 生成组合哈希函数
// 当需要的哈希函数数量超过预定义数量时使用
func generateCombinedHashFunctions(count uint32) []HashFunction {
	functions := make([]HashFunction, count)

	// 先填充预定义的哈希函数
	baseCount := uint32(len(hashFunctions))
	for i := uint32(0); i < baseCount && i < count; i++ {
		functions[i] = hashFunctions[i]
	}

	// 生成额外的组合哈希函数
	for i := baseCount; i < count; i++ {
		seed := i - baseCount
		functions[i] = createCombinedHash(seed)
	}

	return functions
}

// createCombinedHash 创建组合哈希函数
func createCombinedHash(seed uint32) HashFunction {
	return func(data []byte) uint64 {
		// 使用两个基础哈希函数的组合
		h1 := hashFunctions[seed%uint32(len(hashFunctions))](data)
		h2 := hashFunctions[(seed+1)%uint32(len(hashFunctions))](data)

		// 线性组合：h = h1 + seed * h2
		return h1 + uint64(seed)*h2
	}
}

// DoubleHashing 双重哈希（用于开放定址）
type DoubleHashing struct {
	hash1 HashFunction
	hash2 HashFunction
}

// NewDoubleHashing 创建双重哈希
func NewDoubleHashing() *DoubleHashing {
	return &DoubleHashing{
		hash1: hashFNV64,
		hash2: hashDJB2,
	}
}

// Hash 计算第i个哈希值
func (dh *DoubleHashing) Hash(data []byte, i uint32, m uint64) uint64 {
	h1 := dh.hash1(data) % m
	h2 := dh.hash2(data)%m + 1 // 确保h2不为0
	return (h1 + uint64(i)*h2) % m
}

// MurmurHash3 MurmurHash3算法实现
type MurmurHash3 struct {
	seed uint32
}

// NewMurmurHash3 创建MurmurHash3
func NewMurmurHash3(seed uint32) *MurmurHash3 {
	return &MurmurHash3{seed: seed}
}

// Hash64 计算64位哈希值
func (m *MurmurHash3) Hash64(data []byte) uint64 {
	const (
		c1 = 0x87c37b91114253d5
		c2 = 0x4cf5ad432745937f
		r1 = 31
		r2 = 27
		r3 = 33
		m1 = 5
		n1 = 0x52dce729
	)

	length := len(data)
	h := uint64(m.seed)

	// 处理8字节块
	nblocks := length / 8
	for i := 0; i < nblocks; i++ {
		k := binary.LittleEndian.Uint64(data[i*8:])

		k *= c1
		k = rotateLeft64(k, r1)
		k *= c2

		h ^= k
		h = rotateLeft64(h, r2)
		h = h*m1 + n1
	}

	// 处理剩余字节
	tail := data[nblocks*8:]
	var k1 uint64

	switch len(tail) & 7 {
	case 7:
		k1 ^= uint64(tail[6]) << 48
		fallthrough
	case 6:
		k1 ^= uint64(tail[5]) << 40
		fallthrough
	case 5:
		k1 ^= uint64(tail[4]) << 32
		fallthrough
	case 4:
		k1 ^= uint64(tail[3]) << 24
		fallthrough
	case 3:
		k1 ^= uint64(tail[2]) << 16
		fallthrough
	case 2:
		k1 ^= uint64(tail[1]) << 8
		fallthrough
	case 1:
		k1 ^= uint64(tail[0])
		k1 *= c1
		k1 = rotateLeft64(k1, r1)
		k1 *= c2
		h ^= k1
	}

	// 最终化
	h ^= uint64(length)
	h = fmix64(h)

	return h
}

// rotateLeft64 64位左旋转
func rotateLeft64(x uint64, r uint8) uint64 {
	return (x << r) | (x >> (64 - r))
}

// fmix64 最终混合函数
func fmix64(k uint64) uint64 {
	k ^= k >> 33
	k *= 0xff51afd7ed558ccd
	k ^= k >> 33
	k *= 0xc4ceb9fe1a85ec53
	k ^= k >> 33
	return k
}

// ConsistentHash 一致性哈希（用于分布式布隆过滤器）
type ConsistentHash struct {
	hashFunc HashFunction
	replicas int
}

// NewConsistentHash 创建一致性哈希
func NewConsistentHash(replicas int) *ConsistentHash {
	return &ConsistentHash{
		hashFunc: hashFNV64,
		replicas: replicas,
	}
}

// Hash 计算一致性哈希值
func (ch *ConsistentHash) Hash(key string, nodes []string) string {
	if len(nodes) == 0 {
		return ""
	}

	keyHash := ch.hashFunc([]byte(key))
	minDistance := ^uint64(0)
	var selectedNode string

	for _, node := range nodes {
		for i := 0; i < ch.replicas; i++ {
			nodeKey := fmt.Sprintf("%s:%d", node, i)
			nodeHash := ch.hashFunc([]byte(nodeKey))

			var distance uint64
			if nodeHash >= keyHash {
				distance = nodeHash - keyHash
			} else {
				distance = (^uint64(0) - keyHash) + nodeHash + 1
			}

			if distance < minDistance {
				minDistance = distance
				selectedNode = node
			}
		}
	}

	return selectedNode
}
