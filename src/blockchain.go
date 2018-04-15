package main

import (
	"encoding/hex"
	"fmt"
	"github.com/boltdb/bolt"
	"log"
	"os"
)

//BoltDB简介：https://github.com/boltdb/bolt
//BoltDB是一个纯键值存储的DB，没有行和列
//键值对被存储在bucket中，这样就可以将相似的键值对进行分组
//为了获取一个值，你需要知道一个bucket和一个key
//BoltDB没有数据类型，键和值都是byte array
//在BoltDB中，有两种形式的事务：1.db.Update()：读写事务  2.db.View()：只读事务
const dbFile = "blockchain.db"
const blockBucket = "blocks"
const genesisCoinbaseData = "The Times 07/Api/2018 Chancellor on brink of second bailout for banks"

type BlockChain struct {
	tip []byte
	db  *bolt.DB
}

type BlockChainIterator struct {
	currentHash []byte
	db          *bolt.DB
}

func (bc *BlockChain) MineBlock(transactions []*Transaction) {
	var lastHash []byte

	err := bc.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blockBucket))
		lastHash = b.Get([]byte("1"))

		return nil
	})

	if err != nil {
		log.Panic(err)
	}

	newBlock := NewBlock(transactions, lastHash)

	err = bc.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blockBucket))
		err := b.Put(newBlock.Hash, newBlock.Serialize())
		if err != nil {
			log.Panic(err)
		}

		err = b.Put([]byte("1"), newBlock.Hash)
		if err != nil {
			log.Panic(err)
		}

		bc.tip = newBlock.Hash

		return nil
	})
}

//区块链的迭代
//迭代器的初始状态为链中的tip，因此区块将从尾到头进行获取
func (bc *BlockChain) Iterator() *BlockChainIterator {
	bci := &BlockChainIterator{bc.tip, bc.db}

	return bci
}

func (i *BlockChainIterator) Next() *Block {
	var block *Block

	err := i.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blockBucket))
		encodedBlock := b.Get(i.currentHash)
		block = DeserializeBlock(encodedBlock)

		return nil
	})

	if err != nil {
		log.Panic(err)
	}

	i.currentHash = block.PrevBlockHash

	return block
}

func dbExists() bool {
	if _, err := os.Stat(dbFile); os.IsNotExist(err) {
		return false
	}
	return true
}

//1.检查DB中是否有一个区块链
//2.如果存在，打开DB
//3.获取存储区块的bucket，并将1键更新为存储连中的最后一个块的hash
func NewBlockChain(address string) *BlockChain {
	if dbExists() == false {
		fmt.Println("No existing blockchain found. Create one first.")
		os.Exit(1)
	}
	var tip []byte
	db, err := bolt.Open(dbFile, 0600, nil)
	if err != nil {
		log.Panic(err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blockBucket))
		tip = b.Get([]byte("1"))

		return nil
	})

	if err != nil {
		log.Panic(err)
	}

	bc := BlockChain{tip, db}
	return &bc
}

func CreateBlockChain(address string) *BlockChain {
	if dbExists() {
		fmt.Println("Blockchain already exists.")
		os.Exit(1)
	}

	var tip []byte
	db, err := bolt.Open(dbFile, 0600, nil)
	if err != nil {
		log.Panic(err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		cbtx := NewCoinbaseTX(address, genesisCoinbaseData)
		genesis := NewGenesisBlock(cbtx)

		b, err := tx.CreateBucket([]byte(blockBucket))
		if err != nil {
			log.Panic(err)
		}

		err = b.Put(genesis.Hash, genesis.Serialize())
		if err != nil {
			log.Panic(err)
		}

		err = b.Put([]byte("1"), genesis.Hash)
		if err != nil {
			log.Panic(err)
		}
		tip = genesis.Hash

		return nil
	})
	if err != nil {
		log.Panic(err)
	}

	bc := BlockChain{tip, db}

	return &bc
}

//找到所有未花费的输出，并确保它们有足够的价值
//会创建两个输出：
//	1.一个由接收者地址锁定。这是给其他地址实际转移的币
//	2.一个有发送者地址锁定。这是一个找零。只有当未花费输出超过新交易所需时产生。而输出是不可再分的
func (bc *BlockChain) FindUnspentTransactions(address string) []Transaction {
	var unspentTXs []Transaction
	spentTXOs := make(map[string][]int)
	bci := bc.Iterator()

	for {
		block := bci.Next()

		for _, tx := range block.Transactions {
			txID := hex.EncodeToString(tx.ID)

		Outputs:
			for outIDx, out := range tx.Vout {
				//如果一个输出被一个地址锁定，并且这个地址恰好是我们要找的地址，那么这个输出就是我们想要的
				//但在获取它之前，我们需要检查该输出是否以及被包含在一个交易的输出中，即检查它是否已经被花费过了
				if spentTXOs[txID] != nil {
					for _, spentOut := range spentTXOs[txID] {
						if spentOut == outIDx {
							continue Outputs
						}
					}
				}

				if out.CanBeUnlockedWith(address) {
					unspentTXs = append(unspentTXs, *tx)
				}
			}

			//排除所有coinbase交易
			if tx.IsCoinbase() == false {
				for _, in := range tx.Vin {
					if in.CanUnlockOutputWith(address) {
						inTxID := hex.EncodeToString(in.Txid)
						spentTXOs[inTxID] = append(spentTXOs[inTxID], in.Vout)
					}
				}
			}
		}
		if len(block.PrevBlockHash) == 0 {
			break
		}
	}

	return unspentTXs
}

//获取余额需要扫描整个区块链，同时如果我们想要验证后续交易，也需要花费很长时间
//找到UTXO集会加快交易相关的操作
func (bc *BlockChain) FindUTXO(address string) []TXOutput {
	var UTXOs []TXOutput
	unspentTransactions := bc.FindUnspentTransactions(address)

	for _, tx := range unspentTransactions {
		for _, out := range tx.Vout {
			if out.CanBeUnlockedWith(address) {
				UTXOs = append(UTXOs, out)
			}
		}
	}

	return UTXOs
}

//找到有所需数量的输出
//对所有未花费交易进行迭代，并对它的值进行累加，当累加值超过我们想要的值时，返回。
func (bc *BlockChain) FindSpendableOutputs(address string, amount int) (int, map[string][]int) {
	unspentOutputs := make(map[string][]int)
	unspentTXs := bc.FindUnspentTransactions(address)
	accumulated := 0

Work:
	for _, tx := range unspentTXs {
		txID := hex.EncodeToString(tx.ID)

		for outIdx, out := range tx.Vout {
			if out.CanBeUnlockedWith(address) && accumulated < amount {
				accumulated += out.Value
				unspentOutputs[txID] = append(unspentOutputs[txID], outIdx)

				if accumulated >= amount {
					break Work
				}
			}
		}
	}

	return accumulated, unspentOutputs
}

//version0.1
/*
//1.打开一个DB文件
//2.检查是否已经存储了一个区块链
//3.如果已经存储了一个区块链：
//	i.传建一个新的Blockchain实例
//	ii.设置blockchain实例的tip为DB中存储的最后一个块的hash
//4.如果没有区块链：
//	i.创建创世块，存储到DB中
//	ii.将创世块hash保存为最后一个块的hash
//	iii.创建一个新的blockchain实例，初始时tip指向创世块

func NewBlockChain() *BlockChain {
	var tip []byte
	db,err := bolt.Open(dbFile,0600,nil)
	if err != nil {
		log.Panic(err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blockBucket))

		if b == nil {
			fmt.Println("No existing blockchain found. Creating a new one...")
			genesis := NewGenesisBlock()

			b,err := tx.CreateBucket([]byte(blockBucket))
			if err != nil {
				log.Panic(err)
			}

			err = b.Put(genesis.Hash,genesis.Serialize())
			if err != nil {
				log.Panic(err)
			}

			err = b.Put([]byte("1"),genesis.Hash)
			if err != nil {
				log.Panic(err)
			}
			tip = genesis.Hash
		} else {
			tip = b.Get([]byte("1"))
		}

		return nil
	})

	if err != nil {
		log.Panic(err)
	}

	bc := BlockChain{tip,db}

	return &bc
}

//1.先从DB中获取最后一个块的hash
//2.用它来挖出一个新的块的hash
func (bc *BlockChain) AddBlock(data string) {
	var lastHash []byte

	err := bc.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blockBucket))
		lastHash = b.Get([]byte("1"))

		return nil
	})

	if err != nil {
		log.Panic(err)
	}

	newBlock := NewBlock(data,lastHash)

	err = bc.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blockBucket))
		err := b.Put(newBlock.Hash,newBlock.Serialize())
		if err != nil {
			log.Panic(err)
		}

		err = b.Put([]byte("1"),newBlock.Hash)
		if err != nil {
			log.Panic(err)
		}

		bc.tip = newBlock.Hash

		return nil
	})
}

func (bc *BlockChain) Iterator() *BlockChainIterator {
	bci := &BlockChainIterator{bc.tip,bc.db}

	return bci
}

func (i *BlockChainIterator) Next() *Block {
	var block *Block

	err := i.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(blockBucket))
		encodedBlock := b.Get(i.currentHash)
		block = Deserialize(encodedBlock)

		return nil
	})

	if err != nil {
		log.Panic(err)
	}

	i.currentHash = block.PrevBlockHash

	return block
}*/
