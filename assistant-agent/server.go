package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strings"
	"time"
)

type HTTPServer struct {
	agent *Agent
	store *ConfigStore
	token string
}

func NewHTTPServer(agent *Agent, store *ConfigStore) (*HTTPServer, error) {
	raw, err := os.ReadFile(adminTokenPath)
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(string(raw))
	if len(token) < 16 {
		return nil, fmt.Errorf("invalid admin token")
	}
	return &HTTPServer{agent: agent, store: store, token: token}, nil
}

func (s *HTTPServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/", s.auth(s.handleIndex))
	mux.HandleFunc("/api/status", s.auth(s.handleStatus))
	mux.HandleFunc("/api/logs", s.auth(s.handleLogs))
	mux.HandleFunc("/api/config", s.auth(s.handleConfig))
	mux.HandleFunc("/api/wake", s.auth(s.handleWake))
	mux.HandleFunc("/api/text", s.auth(s.handleText))
	mux.HandleFunc("/api/speak", s.auth(s.handleSpeak))
	mux.HandleFunc("/api/reload", s.auth(s.handleReload))
	return requestLogger(mux)
}

func (s *HTTPServer) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-Admin-Token")
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		if token != s.token {
			http.Error(w, "admin token required", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (s *HTTPServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = template.Must(template.New("ui").Parse(indexHTML)).Execute(w, map[string]string{"Token": s.token})
}

func (s *HTTPServer) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.agent.Status())
}
func (s *HTTPServer) handleLogs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"logs": s.agent.Logs()})
}

type configEnvelope struct {
	Config     Config            `json:"config"`
	Secrets    map[string]string `json:"secrets,omitempty"`
	Configured map[string]bool   `json:"configured,omitempty"`
}

func (s *HTTPServer) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, secrets := s.store.Snapshot()
		writeJSON(w, configEnvelope{Config: cfg, Configured: map[string]bool{"asr": secrets.ASRAPIKey != "", "llm": secrets.LLMAPIKey != "", "music": secrets.MusicAPIKey != "", "tts": secrets.TTSAPIKey != ""}})
	case http.MethodPost:
		var in configEnvelope
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.store.Save(in.Config, in.Secrets); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.agent.refreshConfigured()
		writeJSON(w, map[string]any{"ok": true})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *HTTPServer) handleWake(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.agent.TriggerWake("web")
	writeJSON(w, map[string]any{"ok": true})
}

func (s *HTTPServer) handleText(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx, cancel := contextWithTimeout(r, 120*time.Second)
	defer cancel()
	answer, _, err := s.agent.routeText(ctx, strings.TrimSpace(in.Text), false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "answer": answer})
}

func (s *HTTPServer) handleSpeak(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cfg, secrets := s.store.Snapshot()
	ctx, cancel := contextWithTimeout(r, 120*time.Second)
	defer cancel()
	if err := (TTSClient{}).Speak(ctx, cfg, secrets, strings.TrimSpace(in.Text)); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *HTTPServer) handleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := s.store.Reload(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.agent.refreshConfigured()
	writeJSON(w, map[string]any{"ok": true})
}

func contextWithTimeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(value)
}

const indexHTML = `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>L06A Assistant</title><style>
body{font-family:system-ui,sans-serif;margin:0;background:#f5f7fa;color:#1f2937}main{max-width:920px;margin:auto;padding:24px}.card{background:#fff;border-radius:14px;padding:18px;margin:14px 0;box-shadow:0 2px 12px #0001}h1{margin:0 0 8px}label{display:block;margin:10px 0 4px;font-size:13px;color:#4b5563}input,textarea{box-sizing:border-box;width:100%;padding:9px;border:1px solid #cbd5e1;border-radius:8px}button{padding:9px 14px;border:0;border-radius:8px;background:#0f766e;color:#fff;margin:8px 8px 0 0;cursor:pointer}.grid{display:grid;grid-template-columns:1fr 1fr;gap:12px}.state{font-weight:700}.muted{color:#64748b;font-size:13px}pre{white-space:pre-wrap;max-height:280px;overflow:auto;background:#0f172a;color:#d1fae5;padding:12px;border-radius:8px}@media(max-width:700px){.grid{grid-template-columns:1fr}}</style></head><body><main>
<h1>L06A 自定义语音助手</h1><div class="muted">配置保存在 /data，可通过网络更新，不需要重新刷机。</div>
<section class="card"><h2>状态</h2><div id="status">加载中…</div><button onclick="wake()">模拟唤醒</button><button onclick="loadLogs()">刷新日志</button><pre id="logs"></pre></section>
<section class="card"><h2>联调</h2><input id="debugText" placeholder="输入一句话，测试 LLM 或音乐路由"><button onclick="sendText()">发送文字</button><button onclick="speakText()">朗读测试</button><pre id="answer"></pre></section>
<section class="card"><h2>首次配置</h2><div class="muted">这里只需要填写你自己的服务凭据；密码框留空表示保留已经保存的值。</div><div class="grid"><div><label>豆包 ASR API Key</label><input id="asrKey" type="password" autocomplete="off"><label>LLM Base URL</label><input id="llmBase" placeholder="https://服务地址/v1"><label>LLM Model</label><input id="llmModel" placeholder="模型名称"></div><div><label>LLM API Key</label><input id="llmKey" type="password" autocomplete="off"><label>MusicFree API Key</label><input id="musicKey" type="password" autocomplete="off"></div></div><button onclick="saveConfig()">保存配置</button><span id="saved" class="muted"></span></section>
<details class="card"><summary><b>高级设置</b>（默认值通常不用改）</summary><div class="grid"><div><label>ASR Resource ID</label><input id="asrResource"><label>TTS Engine（native 或 remote）</label><input id="ttsEngine"><label>TTS Base URL（原厂语音留空）</label><input id="ttsBase"><label>TTS Model</label><input id="ttsModel"><label>TTS Voice</label><input id="ttsVoice"><label>TTS API Key</label><input id="ttsKey" type="password" autocomplete="off"></div><div><label>Music API Base</label><input id="musicBase"><label>Music Quality</label><input id="musicQuality"><label>首次说话等待秒数</label><input id="initialTimeout" type="number"><label>单句话最长秒数</label><input id="utteranceTimeout" type="number"><label>追问等待秒数</label><input id="followupTimeout" type="number"><label>最大连续轮数</label><input id="followupTurns" type="number"><label>整轮会话最长秒数</label><input id="sessionTimeout" type="number"></div></div><label>系统提示词</label><textarea id="prompt" rows="4"></textarea></details>
</main><script>
const token={{.Token}};let cfg=null;const el=id=>document.getElementById(id);const api=(path,opt={})=>fetch(path+(path.includes('?')?'&':'?')+'token='+encodeURIComponent(token),{...opt,headers:{'Content-Type':'application/json','X-Admin-Token':token,...(opt.headers||{})}});
async function refresh(){const s=await (await api('/api/status')).json();el('status').innerHTML='<span class="state">'+s.state+'</span><br>ASR '+s.configured.asr+' · LLM '+s.configured.llm+' · 音乐 '+s.configured.music+'<br>识别：'+(s.last_transcript||'')+'<br>回答：'+(s.last_response||'')+'<br><b style="color:#b91c1c">'+(s.last_error||'')+'</b>'}
async function loadConfig(){try{const r=await api('/api/config');if(!r.ok)throw new Error(await r.text());const x=await r.json();cfg=x.config;el('asrResource').value=cfg.asr_resource_id;el('llmBase').value=cfg.llm_base_url;el('llmModel').value=cfg.llm_model;el('ttsEngine').value=cfg.tts_engine;el('ttsBase').value=cfg.tts_base_url;el('ttsModel').value=cfg.tts_model;el('ttsVoice').value=cfg.tts_voice;el('musicBase').value=cfg.music_api_base;el('musicQuality').value=cfg.music_quality;el('initialTimeout').value=cfg.initial_speech_timeout_sec;el('utteranceTimeout').value=cfg.max_utterance_sec;el('followupTimeout').value=cfg.followup_timeout_sec;el('followupTurns').value=cfg.followup_max_turns;el('sessionTimeout').value=cfg.session_max_sec;el('prompt').value=cfg.llm_system_prompt}catch(e){el('saved').textContent='加载配置失败：'+e.message}}
async function saveConfig(){if(!cfg){el('saved').textContent='配置尚未加载，请刷新页面';return}cfg.asr_resource_id=el('asrResource').value;cfg.llm_base_url=el('llmBase').value;cfg.llm_model=el('llmModel').value;cfg.tts_engine=el('ttsEngine').value;cfg.tts_base_url=el('ttsBase').value;cfg.tts_model=el('ttsModel').value;cfg.tts_voice=el('ttsVoice').value;cfg.music_api_base=el('musicBase').value;cfg.music_quality=el('musicQuality').value;cfg.initial_speech_timeout_sec=+el('initialTimeout').value;cfg.max_utterance_sec=+el('utteranceTimeout').value;cfg.followup_timeout_sec=+el('followupTimeout').value;cfg.followup_max_turns=+el('followupTurns').value;cfg.session_max_sec=+el('sessionTimeout').value;cfg.llm_system_prompt=el('prompt').value;const secrets={ASR_API_KEY:el('asrKey').value,LLM_API_KEY:el('llmKey').value,MUSIC_API_KEY:el('musicKey').value,TTS_API_KEY:el('ttsKey').value};const r=await api('/api/config',{method:'POST',body:JSON.stringify({config:cfg,secrets})});el('saved').textContent=r.ok?'已保存':'保存失败：'+await r.text();el('asrKey').value=el('llmKey').value=el('musicKey').value=el('ttsKey').value='';refresh()}
async function wake(){await api('/api/wake',{method:'POST'});refresh()}
async function sendText(){const r=await api('/api/text',{method:'POST',body:JSON.stringify({text:el('debugText').value})});el('answer').textContent=r.ok?JSON.stringify(await r.json(),null,2):await r.text();refresh()}
async function speakText(){const r=await api('/api/speak',{method:'POST',body:JSON.stringify({text:el('debugText').value})});el('answer').textContent=r.ok?'朗读成功':await r.text();refresh()}
async function loadLogs(){const x=await (await api('/api/logs')).json();el('logs').textContent=(x.logs||[]).join('\n')}
loadConfig();refresh();setInterval(refresh,3000);
</script></body></html>`
