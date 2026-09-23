package task

import (
	"testing"

	"github.com/v03413/bepusdt/app/conf"
)

// blockJSON 是构造的 SPL TransferChecked 区块数据，结构与线上一致。
// 该结构对应 maxSupportedTransactionVersion=0 下会被节点整块拒绝的区块。
const blockJSON = `{"jsonrpc":"2.0","result":{"blockTime":1789884461,"transactions":[
{"transaction":{"signatures":["26UhSeTBkeVrQwpxC2KjxiyYCDEYbCggNZStmXjnwoyJ3tE6eMebjMzNY2a4RXGuaLmS2db3S4MPbdVyigH2TM3g"],
"message":{"accountKeys":["8TEhXChfrZUJqZdN533mdKVH9hh2pLrG5B3tcSVy3NvH","DsKXrcVGb2kn3N3pXxWLunQiWw9VdfsXcVH5UFeeYyPz","4QfHqsmd6JhpSqruvDrgyeTRFHKEKHQM3TxXjDTmZcWH","Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB","ComputeBudget111111111111111111111111111111","TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"],
"instructions":[{"accounts":[],"data":"3gJqkocMWaMm","programIdIndex":4},{"accounts":[],"data":"L22o7M","programIdIndex":4},{"accounts":[1,3,2,0],"data":"g7eSRZwiTurnH","programIdIndex":5}]}},
"meta":{"err":null,"innerInstructions":[],
"preTokenBalances":[{"accountIndex":1,"mint":"Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB","owner":"8TEhXChfrZUJqZdN533mdKVH9hh2pLrG5B3tcSVy3NvH","programId":"TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA","uiTokenAmount":{"amount":"11073831096","decimals":6}},
{"accountIndex":2,"mint":"Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB","owner":"12uk89efFPMXSxpw4stk8m4srLRGYTxKMcgtKd3djXhg","programId":"TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA","uiTokenAmount":{"amount":"2702142829","decimals":6}}],
"postTokenBalances":[{"accountIndex":1,"mint":"Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB","owner":"8TEhXChfrZUJqZdN533mdKVH9hh2pLrG5B3tcSVy3NvH","programId":"TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA","uiTokenAmount":{"amount":"6073831096","decimals":6}},
{"accountIndex":2,"mint":"Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB","owner":"12uk89efFPMXSxpw4stk8m4srLRGYTxKMcgtKd3djXhg","programId":"TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA","uiTokenAmount":{"amount":"7702142829","decimals":6}}]}}
]}}`

const (
	testTxHash     = "26UhSeTBkeVrQwpxC2KjxiyYCDEYbCggNZStmXjnwoyJ3tE6eMebjMzNY2a4RXGuaLmS2db3S4MPbdVyigH2TM3g"
	testRecvWallet = "12uk89efFPMXSxpw4stk8m4srLRGYTxKMcgtKd3djXhg"
	testFromWallet = "8TEhXChfrZUJqZdN533mdKVH9hh2pLrG5B3tcSVy3NvH"
)

func TestParseBlockExtractsSolanaPayment(t *testing.T) {
	s := newSolana()
	got := s.parseBlock([]byte(blockJSON), 448645811)

	if len(got) != 1 {
		t.Fatalf("期望解析出 1 笔转账，实际 %d 笔: %+v", len(got), got)
	}

	tr := got[0]
	if tr.TxHash != testTxHash {
		t.Errorf("交易哈希错误: %s", tr.TxHash)
	}
	if tr.RecvAddress != testRecvWallet {
		t.Errorf("收款地址错误: %s", tr.RecvAddress)
	}
	if tr.FromAddress != testFromWallet {
		t.Errorf("付款地址错误: %s", tr.FromAddress)
	}
	if tr.Amount.String() != "5000" {
		t.Errorf("金额错误: %s", tr.Amount.String())
	}
	if tr.TradeType != "usdt.solana" {
		t.Errorf("交易类型未识别: %s", tr.TradeType)
	}
	if tr.Network != conf.Solana {
		t.Errorf("网络错误: %s", tr.Network)
	}
	if tr.BlockNum != 448645811 {
		t.Errorf("区块号错误: %d", tr.BlockNum)
	}
}

func TestParseBlockEmpty(t *testing.T) {
	const body = `{"jsonrpc":"2.0","result":{"blockTime":1789884461,"transactions":[
{"transaction":{"signatures":["abc"],"message":{"accountKeys":["11111111111111111111111111111111"],"instructions":[]}},"meta":{"err":null,"preTokenBalances":[],"postTokenBalances":[]}}
]}}`

	s := newSolana()
	if got := s.parseBlock([]byte(body), 1); len(got) != 0 {
		t.Fatalf("期望 0 笔转账，实际 %d 笔", len(got))
	}
}
