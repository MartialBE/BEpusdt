package task

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"testing"

	"github.com/tidwall/gjson"
)

const (
	envSolanaRPC     = "BEPUSDT_SOLANA_RPC"
	envSolanaAddress = "BEPUSDT_SOLANA_ADDRESS"
	envSolanaTx      = "BEPUSDT_SOLANA_TX"
	envSolanaScan    = "BEPUSDT_SOLANA_SCAN_BLOCKS"
)

func solanaEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return def
}

func solanaRPC(t *testing.T) string {
	t.Helper()

	url := os.Getenv(envSolanaRPC)
	if url == "" {
		t.Skipf("跳过线上验证：需设置 %s 指定 Solana RPC 节点", envSolanaRPC)
	}
	if n, _ := strconv.Atoi(solanaEnv(envSolanaScan, "0")); n <= 0 {
		t.Skipf("跳过线上验证：设置 %s=<要扫描的最近区块数> 后运行（RPC=%s）", envSolanaScan, url)
	}

	return url
}

func solanaCall(t *testing.T, url, method string, params ...any) gjson.Result {
	t.Helper()

	result, err := solanaRawCall(url, method, params)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if resp := result.Get("__http_status"); resp.Exists() {
		t.Skipf("RPC 返回 HTTP %d（公共节点限流，可用 %s 指定其它节点）：%.200s",
			resp.Int(), envSolanaRPC, result.Get("__body").String())
	}

	return result
}

func solanaRawCall(url, method string, params []any) (gjson.Result, error) {
	if params == nil {
		params = []any{}
	}
	buf, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		return gjson.Result{}, fmt.Errorf("构造请求失败: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(buf))
	if err != nil {
		return gjson.Result{}, fmt.Errorf("构造 HTTP 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := newSolana().client.Do(req)
	if err != nil {
		return gjson.Result{}, fmt.Errorf("请求 %s 失败: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return gjson.Result{}, fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode != 200 {
		return gjson.Parse(fmt.Sprintf(`{"__http_status":%d,"__body":%q}`, resp.StatusCode, string(body))), nil
	}

	return gjson.ParseBytes(body), nil
}

func solanaBlockParams(slot, mv int) []any {
	return []any{slot, map[string]any{
		"encoding": "json", "maxSupportedTransactionVersion": mv,
		"transactionDetails": "full", "rewards": false,
	}}
}

// TestLiveSolanaScanDetectsPayment 从真实 RPC 拉取区块，验证扫描器能解析出指定钱包的收款。
// 需显式提供 BEPUSDT_SOLANA_SCAN_BLOCKS 与 BEPUSDT_SOLANA_ADDRESS，未提供时自动跳过。
func TestLiveSolanaScanDetectsPayment(t *testing.T) {
	url := solanaRPC(t)
	address := os.Getenv(envSolanaAddress)
	if address == "" {
		t.Skipf("跳过线上验证：需设置 %s 指定要校验的收款钱包", envSolanaAddress)
	}
	txHash := os.Getenv(envSolanaTx)
	scan, _ := strconv.Atoi(solanaEnv(envSolanaScan, "0"))

	setupSolanaTestEnv(t)
	setSolanaEndpoint(t, url)

	head := int(solanaCall(t, url, "getSlot").Get("result").Int())
	if head <= 0 {
		t.Fatalf("getSlot 返回异常: %d", head)
	}
	t.Logf("RPC=%s 当前 slot=%d 目标钱包=%s 扫描深度=%d", url, head, address, scan)

	// 指定交易时先定位其所在 slot，避免因链高度在扫描期间推进而错过目标区块
	start := head - 1
	if txHash != "" {
		getTx, err := solanaRawCall(url, "getTransaction", []any{
			txHash, map[string]any{"encoding": "json", "maxSupportedTransactionVersion": 1},
		})
		if err != nil {
			t.Fatalf("%v", err)
		}
		if e := getTx.Get("error"); e.Exists() {
			t.Fatalf("查询交易 %s 失败: %s", txHash, e.String())
		}
		if slot := int(getTx.Get("result.slot").Int()); slot > 0 {
			start = slot
			t.Logf("已定位交易 %s 所在 slot=%d", txHash, slot)
		}
	}

	var (
		scanned int
		found   []transfer
	)
	parser := newSolana()
	for slot := start; slot > start-scan && slot > 0; slot-- {
		body := solanaCall(t, url, "getBlock", solanaBlockParams(slot, 1)...)
		if body.Get("result").Type == gjson.Null {
			continue
		}

		scanned++
		for _, tr := range parser.parseBlock([]byte(body.Raw), slot) {
			if tr.RecvAddress != address {
				continue
			}
			found = append(found, tr)
			t.Logf("命中 slot=%d tx=%s 金额=%s 来源=%s", slot, tr.TxHash, tr.Amount.String(), tr.FromAddress)
		}
	}

	t.Logf("共扫描 %d 个区块，命中 %d 笔发往 %s 的收款", scanned, len(found), address)
	if txHash != "" {
		for _, tr := range found {
			if tr.TxHash == txHash {
				t.Logf("已按交易哈希确认: %s", txHash)

				return
			}
		}
		t.Fatalf("指定了交易 %s，但自其 slot 起回扫 %d 个区块未解析到（命中 %d 笔）",
			txHash, scan, len(found))
	}

	if len(found) == 0 {
		t.Skipf("最近 %d 个区块内没有发往 %s 的收款（可增大 %s，或用 %s 指定交易）",
			scan, address, envSolanaScan, envSolanaTx)
	}
}

// TestLiveSolanaBlockAcceptsMaxSupportedVersion 验证指定 RPC 在 mv=1 下可取块，
// 并探测同一区块在 mv=0 下是否被整块拒绝——即线上漏单的成因。
func TestLiveSolanaBlockAcceptsMaxSupportedVersion(t *testing.T) {
	url := solanaRPC(t)

	head := int(solanaCall(t, url, "getSlot").Get("result").Int())
	if head <= 0 {
		t.Fatalf("getSlot 返回异常: %d", head)
	}
	slot := head - 400

	get := func(mv int) (bool, string) {
		body, err := solanaRawCall(url, "getBlock", solanaBlockParams(slot, mv))
		if err != nil {
			t.Fatalf("%v", err)
		}
		if e := body.Get("error"); e.Exists() {
			return false, e.Get("message").String()
		}

		return true, strconv.Itoa(len(body.Get("result.transactions").Array())) + " 笔交易"
	}

	ok, msg := get(1)
	if !ok {
		t.Fatalf("slot %d 在 mv=1 下应能获取，实际失败: %s", slot, msg)
	}
	t.Logf("slot %d mv=1 -> OK (%s)", slot, msg)

	if ok, msg := get(0); !ok {
		t.Logf("slot %d mv=0 -> 被整块拒绝（复现线上成因）: %s", slot, msg)
	} else {
		t.Logf("slot %d mv=0 -> 恰好可取 (%s)，该区块不含 v1 交易", slot, msg)
	}
}
