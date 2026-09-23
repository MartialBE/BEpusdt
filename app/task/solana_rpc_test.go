package task

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/v03413/bepusdt/app/conf"
	"github.com/v03413/bepusdt/app/log"
	"github.com/v03413/bepusdt/app/model"
	"gorm.io/gorm"
)

func setupSolanaTestEnv(t *testing.T) {
	t.Helper()

	if err := log.Init(t.TempDir()); err != nil {
		t.Fatalf("初始化日志失败: %v", err)
	}

	if model.Db == nil {
		// 数据库文件必须比单个测试用例存活更久，因此放进包级临时目录而非 t.TempDir()
		dir, err := os.MkdirTemp("", "bepusdt-solana-test-")
		if err != nil {
			t.Fatalf("创建临时目录失败: %v", err)
		}
		db, err := gorm.Open(sqlite.Open(filepath.Join(dir, "t.db")), &gorm.Config{})
		if err != nil {
			t.Fatalf("打开测试数据库失败: %v", err)
		}
		if err := db.AutoMigrate(&model.Conf{}); err != nil {
			t.Fatalf("迁移配置表失败: %v", err)
		}
		model.Db = db
	}
	model.RefreshC()
}

// TestSlotParseHonoursRpcRequestAndQueue 驱动真实 HTTP 链路：断言请求携带
// maxSupportedTransactionVersion=1，并断言区块内的收款被投递到 transferQueue。
func TestSlotParseHonoursRpcRequestAndQueue(t *testing.T) {
	setupSolanaTestEnv(t)

	var (
		mu      sync.Mutex
		seenVer []float64
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		var opts struct {
			MaxSupportedTransactionVersion *float64 `json:"maxSupportedTransactionVersion"`
		}
		if len(req.Params) > 1 {
			_ = json.Unmarshal(req.Params[1], &opts)
		}
		if opts.MaxSupportedTransactionVersion != nil {
			mu.Lock()
			seenVer = append(seenVer, *opts.MaxSupportedTransactionVersion)
			mu.Unlock()
		}

		if opts.MaxSupportedTransactionVersion == nil || *opts.MaxSupportedTransactionVersion == 0 {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32015,"message":"Transaction version (1) is not supported by the requesting client."}}`))

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(blockJSON))
	}))
	defer srv.Close()

	setSolanaEndpoint(t, srv.URL)

	// 消费 transferQueue，接收 parseBlock 投递的转账
	got := make(chan transfer, 1)
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			case batch := <-transferQueue.Out:
				for _, tr := range batch {
					select {
					case got <- tr:
					default:
					}
				}
			}
		}
	}()
	defer close(stop)

	s := newSolana()
	s.slotParse(448645811)

	mu.Lock()
	vers := append([]float64(nil), seenVer...)
	mu.Unlock()

	if len(vers) == 0 {
		t.Fatal("未捕获到 getBlock 请求")
	}
	for _, v := range vers {
		if v != 1 {
			t.Fatalf("getBlock 请求的 maxSupportedTransactionVersion = %v，期望 1", v)
		}
	}

	select {
	case tr := <-got:
		if tr.Amount.String() != "5000" || tr.RecvAddress != testRecvWallet {
			t.Fatalf("投递的转账不正确: %s -> %s, 金额 %s", tr.FromAddress, tr.RecvAddress, tr.Amount.String())
		}
	default:
		t.Fatal("区块已成功解析，但收款未投递到 transferQueue")
	}
}

// TestSlotParseRejectsRpcErrorBody 掩盖故障的根源之一：HTTP 200 携带 JSON-RPC 错误时，
// 必须记为失败且不得计入成功率。
func TestSlotParseRejectsRpcErrorBody(t *testing.T) {
	setupSolanaTestEnv(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32015,"message":"boom"}}`))
	}))
	defer srv.Close()

	setSolanaEndpoint(t, srv.URL)

	e := newSolana()
	e.slotParse(9)

	if r := getSolanaSuccessRate(t); r == "100.00%" {
		t.Fatalf("HTTP 200 + JSON-RPC 错误 未被记为失败，成功率仍为 %s", r)
	}
}

func setSolanaEndpoint(t *testing.T, url string) {
	t.Helper()

	if err := model.Db.Where("k = ?", model.RpcEndpointSolana).Delete(&model.Conf{}).Error; err != nil {
		t.Fatalf("清理旧配置失败: %v", err)
	}
	if err := model.Db.Create(&model.Conf{K: model.RpcEndpointSolana, V: url}).Error; err != nil {
		t.Fatalf("写入 RPC 配置失败: %v", err)
	}
	model.RefreshC()
}

func getSolanaSuccessRate(t *testing.T) string {
	t.Helper()

	return conf.GetSuccessRate(conf.Solana)
}
