package util

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var (
	ErrInvalidLevel   = errors.New("invalid level")
	ErrInvalidHeight  = errors.New("invalid height")
	ErrMisaligned     = errors.New("misaligned")
	ErrIncorrectProof = errors.New("incorrect proof")
)

type MerkleExpansion []common.Hash

func NewEmptyMerkleExpansion() MerkleExpansion {
	return []common.Hash{}
}

func (me MerkleExpansion) Clone() MerkleExpansion {
	return append([]common.Hash{}, me...)
}

func (me MerkleExpansion) Root() common.Hash {
	accum := common.Hash{}
	empty := true
	for _, h := range me {
		if empty {
			if h != (common.Hash{}) {
				empty = false
				accum = h
			}
		} else {
			accum = crypto.Keccak256Hash(accum.Bytes(), h.Bytes())
		}
	}
	return accum
}

func (me MerkleExpansion) Compact() ([]common.Hash, uint64) {
	comp := []common.Hash{}
	size := uint64(0)
	for level, h := range me {
		if h != (common.Hash{}) {
			comp = append(comp, h)
			size += 1 << level
		}
	}
	return comp, size
}

func MerkleExpansionFromCompact(comp []common.Hash, size uint64) (MerkleExpansion, uint64) {
	me := []common.Hash{}
	numRead := uint64(0)
	i := uint64(1)
	for i <= size {
		if i&size != 0 {
			numRead++
			me = append(me, comp[0])
			comp = comp[1:]
		} else {
			me = append(me, common.Hash{})
		}
		i <<= 1
	}
	return me, numRead
}

func (me MerkleExpansion) AppendCompleteSubtree(level uint64, hash common.Hash) (MerkleExpansion, error) {
	if len(me) == 0 {
		exp := make([]common.Hash, level+1)
		exp[level] = hash
		return exp, nil
	}
	if level >= uint64(len(me)) {
		fmt.Println(level, len(me))
		return nil, ErrInvalidLevel
	}
	ret := me.Clone()
	for i := uint64(0); i < uint64(len(me)); i++ {
		if i < level {
			if ret[i] != (common.Hash{}) {
				return nil, ErrMisaligned
			}
		} else {
			if ret[i] == (common.Hash{}) {
				ret[i] = hash
				return ret, nil
			} else {
				hash = crypto.Keccak256Hash(ret[i].Bytes(), hash.Bytes())
				ret[i] = common.Hash{}
			}
		}
	}
	return append(ret, hash), nil
}

func (me MerkleExpansion) AppendLeaf(leafHash common.Hash) MerkleExpansion {
	ret, _ := me.AppendCompleteSubtree(0, crypto.Keccak256Hash(leafHash.Bytes())) // re-hash to avoid collision with internal node hash
	return ret
}

func VerifyProof(pre, post HistoryCommitment, compactPre []common.Hash, proof common.Hash) error {
	preExpansion, _ := MerkleExpansionFromCompact(compactPre, pre.Height)
	if pre.Height >= post.Height {
		return ErrInvalidHeight
	}
	diff := post.Height - pre.Height
	if bits.OnesCount64(diff) != 1 {
		return ErrMisaligned
	}
	level := bits.TrailingZeros64(diff)
	postExpansion, err := preExpansion.AppendCompleteSubtree(uint64(level), proof)
	if err != nil {
		return err
	}
	if postExpansion.Root() != post.Merkle {
		return ErrIncorrectProof
	}
	return nil
}

func VerifyPrefixProof(pre, post HistoryCommitment, proof []common.Hash) error {
	if pre.Height >= post.Height {
		return ErrInvalidHeight
	}
	expHeight := pre.Height
	expansion, numRead := MerkleExpansionFromCompact(proof, expHeight)
	proof = proof[numRead:]
	for expHeight < post.Height {
		if len(proof) == 0 {
			return ErrIncorrectProof
		}
		// extHeight looks like   xxxxxxx0yyy
		// post.height looks like xxxxxxx1zzz
		firstDiffBit := 63 - bits.LeadingZeros64(expHeight^post.Height)
		mask := (uint64(1) << firstDiffBit) - 1
		yyy := expHeight & mask
		zzz := post.Height & mask
		if yyy != 0 {
			lowBit := bits.TrailingZeros64(yyy)
			exp, err := expansion.AppendCompleteSubtree(uint64(lowBit), proof[0])
			if err != nil {
				return err
			}
			expansion = exp
			expHeight += 1 << lowBit
			proof = proof[1:]
		} else if zzz != 0 {
			highBit := 63 - bits.LeadingZeros64(zzz)
			exp, err := expansion.AppendCompleteSubtree(uint64(highBit), proof[0])
			if err != nil {
				return err
			}
			expansion = exp
			expHeight += 1 << highBit
			proof = proof[1:]
		} else {
			exp, err := expansion.AppendCompleteSubtree(uint64(firstDiffBit), proof[0])
			if err != nil {
				return err
			}
			expansion = exp
			expHeight = post.Height
			proof = proof[1:]
		}
	}
	if expansion.Root() != post.Merkle {
		return ErrIncorrectProof
	}
	return nil
}

func GeneratePrefixProof(preHeight uint64, preExpansion MerkleExpansion, leaves []common.Hash) []common.Hash {
	height := preHeight
	postHeight := height + uint64(len(leaves))
	proof, _ := preExpansion.Compact()
	for height < postHeight {
		// extHeight looks like   xxxxxxx0yyy
		// post.height looks like xxxxxxx1zzz
		firstDiffBit := 63 - bits.LeadingZeros64(height^postHeight)
		mask := (uint64(1) << firstDiffBit) - 1
		yyy := height & mask
		zzz := postHeight & mask
		if yyy != 0 {
			lowBit := bits.TrailingZeros64(yyy)
			numLeaves := uint64(1) << lowBit
			proof = append(proof, ExpansionFromLeaves(leaves[:numLeaves]).Root())
			leaves = leaves[numLeaves:]
			height += numLeaves
		} else if zzz != 0 {
			highBit := 63 - bits.LeadingZeros64(zzz)
			numLeaves := uint64(1) << highBit
			proof = append(proof, ExpansionFromLeaves(leaves[:numLeaves]).Root())
			leaves = leaves[numLeaves:]
			height += numLeaves
		} else {
			proof = append(proof, ExpansionFromLeaves(leaves).Root())
			height = postHeight
		}
	}
	return proof
}

func ExpansionFromLeaves(leaves []common.Hash) MerkleExpansion {
	ret := NewEmptyMerkleExpansion()
	for _, leaf := range leaves {
		ret = ret.AppendLeaf(leaf)
	}
	return ret
}

func HashForUint(x uint64) common.Hash {
	return crypto.Keccak256Hash(binary.BigEndian.AppendUint64([]byte{}, x))
}
