package rest

import (
	"bytes"
	"encoding/json"
	"strings"
)

// betaCallConsoleMarker marks the injected overlay so it is added at most once.
const betaCallConsoleMarker = "data-gowa-beta-call"

// InjectBetaCallConsole appends a small beta voice-call panel to the dashboard
// HTML. Callers decide when to use it (the beta flag); the panel talks to the
// same REST endpoints as the API, so it needs no dashboard rebuild and survives
// dashboard updates.
func InjectBetaCallConsole(html []byte, basePath string) []byte {
	if bytes.Contains(html, []byte(betaCallConsoleMarker)) {
		return html
	}

	script := []byte(betaCallConsoleScript(basePath))
	if idx := bytes.LastIndex(html, []byte("</body>")); idx >= 0 {
		out := make([]byte, 0, len(html)+len(script))
		out = append(out, html[:idx]...)
		out = append(out, script...)
		return append(out, html[idx:]...)
	}

	return append(html, script...)
}

func betaCallConsoleScript(basePath string) string {
	encodedBase, err := json.Marshal(basePath)
	if err != nil {
		// basePath is a plain string, so json.Marshal cannot fail here.
		encodedBase = []byte(`""`)
	}

	return strings.ReplaceAll(betaCallConsoleTemplate, "{{BASE}}", string(encodedBase))
}

const betaCallConsoleTemplate = `<script data-gowa-beta-call>
(function () {
  if (document.getElementById('gowa-beta-call-panel')) return;

  var base = {{BASE}};
  var style = document.createElement('style');
  style.textContent =
    '#gowa-beta-call-toggle{position:fixed;right:16px;bottom:16px;z-index:2147483000;' +
    'padding:8px 12px;border:1px solid ButtonBorder;border-radius:999px;' +
    'background:ButtonFace;color:ButtonText;font:600 12px system-ui,sans-serif;cursor:pointer}' +
    '#gowa-beta-call-panel{position:fixed;right:16px;bottom:60px;z-index:2147483000;width:320px;' +
    'display:flex;flex-direction:column;gap:8px;padding:12px;border:1px solid ButtonBorder;' +
    'border-radius:12px;background:Canvas;color:CanvasText;' +
    'box-shadow:0 10px 30px rgba(0,0,0,.25);font:12px/1.4 system-ui,sans-serif}' +
    '#gowa-beta-call-panel[hidden]{display:none}' +
    '#gowa-beta-call-panel input{width:100%;padding:6px;border:1px solid ButtonBorder;' +
    'border-radius:6px;background:Field;color:FieldText;font:inherit}' +
    '#gowa-beta-call-actions{display:flex;flex-wrap:wrap;gap:6px}' +
    '#gowa-beta-call-actions button{flex:1 1 30%;padding:6px;border:1px solid ButtonBorder;' +
    'border-radius:6px;background:ButtonFace;color:ButtonText;font:inherit;cursor:pointer}' +
    '#gowa-beta-call-banner{padding:6px 8px;border-radius:6px;background:#fff4ce;color:#4a3b00;' +
    'font-weight:600}' +
    '#gowa-beta-call-banner[hidden]{display:none}' +
    '#gowa-beta-call-log{max-height:140px;overflow:auto;margin:0;padding:6px;' +
    'border-radius:6px;background:Field;color:FieldText;white-space:pre-wrap;word-break:break-all}';
  document.head.appendChild(style);

  var toggle = document.createElement('button');
  toggle.type = 'button';
  toggle.id = 'gowa-beta-call-toggle';
  toggle.textContent = 'Beta call';

  var panel = document.createElement('div');
  panel.id = 'gowa-beta-call-panel';
  panel.hidden = true;
  panel.innerHTML =
    '<strong>Beta voice call</strong>' +
    '<div id="gowa-beta-call-banner" hidden></div>' +
    '<input id="gowa-beta-call-device" placeholder="device id (blank = default)">' +
    '<input id="gowa-beta-call-target" placeholder="target, e.g. 628123456789">' +
    '<input id="gowa-beta-call-audio" placeholder="audio file path (.mp3/.wav, optional)">' +
    '<input id="gowa-beta-call-record" placeholder="record peer audio to .wav (optional)">' +
    '<input id="gowa-beta-call-id" placeholder="call id">' +
    '<div id="gowa-beta-call-actions">' +
    '<button type="button" id="gowa-beta-call-start">Start</button>' +
    '<button type="button" id="gowa-beta-call-answer">Answer</button>' +
    '<button type="button" id="gowa-beta-call-hangup">Hangup</button>' +
    '<button type="button" id="gowa-beta-call-live">Live audio</button>' +
    '</div>' +
    '<pre id="gowa-beta-call-log"></pre>';

  document.body.appendChild(toggle);
  document.body.appendChild(panel);

  var device = document.getElementById('gowa-beta-call-device');
  var target = document.getElementById('gowa-beta-call-target');
  var audio = document.getElementById('gowa-beta-call-audio');
  var record = document.getElementById('gowa-beta-call-record');
  var callID = document.getElementById('gowa-beta-call-id');
  var liveButton = document.getElementById('gowa-beta-call-live');
  var banner = document.getElementById('gowa-beta-call-banner');
  var logEl = document.getElementById('gowa-beta-call-log');
  var live = null;
  var incomingWS = null;
  var incomingRetry = null;
  var refreshSuppressed = null;

  // AudioWorklet conversions between the browser's float samples and the
  // s16le 16 kHz mono frames the call bridge speaks. Kept inline so the panel
  // needs no extra asset route.
  var workletSource = [
    "class GowaMic extends AudioWorkletProcessor {",
    "  constructor() { super(); this.buf = new Float32Array(960); this.n = 0; }",
    "  process(inputs) {",
    "    var input = inputs[0] && inputs[0][0];",
    "    if (!input) return true;",
    "    for (var i = 0; i < input.length; i++) {",
    "      this.buf[this.n++] = input[i];",
    "      if (this.n < 960) continue;",
    "      var pcm = new Int16Array(960);",
    "      for (var j = 0; j < 960; j++) {",
    "        var s = Math.max(-1, Math.min(1, this.buf[j]));",
    "        pcm[j] = s < 0 ? s * 0x8000 : s * 0x7fff;",
    "      }",
    "      this.port.postMessage(pcm, [pcm.buffer]);",
    "      this.n = 0;",
    "    }",
    "    return true;",
    "  }",
    "}",
    "class GowaSpeaker extends AudioWorkletProcessor {",
    "  constructor() {",
    "    super(); this.queue = []; this.offset = 0;",
    "    this.port.onmessage = (e) => {",
    "      this.queue.push(e.data);",
    "      if (this.queue.length > 20) this.queue.shift();",
    "    };",
    "  }",
    "  process(inputs, outputs) {",
    "    var out = outputs[0] && outputs[0][0];",
    "    if (!out) return true;",
    "    var i = 0;",
    "    while (i < out.length && this.queue.length) {",
    "      var chunk = this.queue[0];",
    "      var n = Math.min(out.length - i, chunk.length - this.offset);",
    "      for (var j = 0; j < n; j++) out[i + j] = chunk[this.offset + j] / 32768;",
    "      i += n; this.offset += n;",
    "      if (this.offset >= chunk.length) { this.queue.shift(); this.offset = 0; }",
    "    }",
    "    for (; i < out.length; i++) out[i] = 0;",
    "    return true;",
    "  }",
    "}",
    "registerProcessor('gowa-mic', GowaMic);",
    "registerProcessor('gowa-speaker', GowaSpeaker);"
  ].join('\n');

  // The fallback page reloads every 30s. Pull its <meta> while a call is live so
  // a refresh cannot tear down the mic stream or the controls, then restore it.
  // The downloaded dashboard has no refresh tag, so this is a no-op there.
  function setCallActive(active) {
    var meta = document.querySelector('meta[http-equiv="refresh"]');
    if (active) {
      if (refreshSuppressed || !meta) return;
      refreshSuppressed = meta.getAttribute('content') || '30';
      meta.remove();
      return;
    }
    if (!refreshSuppressed) return;
    var restored = document.createElement('meta');
    restored.httpEquiv = 'refresh';
    restored.content = refreshSuppressed;
    document.head.appendChild(restored);
    refreshSuppressed = null;
  }

  function selectedDevice() {
    try {
      var stored = localStorage.getItem('gowa-ui.device.v1');
      var state = stored ? JSON.parse(stored).state : null;
      return (state && state.selectedDeviceId) || '';
    } catch (err) {
      return '';
    }
  }

  function currentDevice() {
    return device.value.trim() || selectedDevice();
  }

  function log(message) {
    var stamped = new Date().toISOString().slice(11, 19) + '  ' + message;
    logEl.textContent = stamped + '\n' + logEl.textContent;
  }

  async function post(action, body) {
    var headers = {'Content-Type': 'application/json'};
    var deviceID = currentDevice();
    if (deviceID) headers['X-Device-Id'] = encodeURIComponent(deviceID);

    var response = await fetch(base + '/call/' + action, {
      method: 'POST',
      headers: headers,
      body: JSON.stringify(body)
    });
    var payload = await response.text();
    try {
      payload = JSON.parse(payload);
    } catch (err) {
      // Non-JSON error pages are still worth showing verbatim.
    }
    log(action + ' ' + response.status + ' ' + JSON.stringify(payload));
    if (!response.ok) {
      throw new Error((payload && payload.message) || response.statusText);
    }
    return payload;
  }

  async function startLive() {
    if (live) return;
    var id = callID.value.trim();
    if (!id) throw new Error('call id required: start or answer a call first');
    if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) {
      throw new Error('microphone needs a secure context (https or localhost)');
    }

    var ctx = new AudioContext({sampleRate: 16000});
    if (ctx.sampleRate !== 16000) {
      await ctx.close();
      throw new Error('browser forced ' + ctx.sampleRate + ' Hz; 16 kHz needed');
    }

    var workletURL = URL.createObjectURL(new Blob([workletSource], {type: 'application/javascript'}));
    try {
      await ctx.audioWorklet.addModule(workletURL);
    } finally {
      URL.revokeObjectURL(workletURL);
    }

    var stream;
    try {
      stream = await navigator.mediaDevices.getUserMedia({
        audio: {channelCount: 1, echoCancellation: true, noiseSuppression: true}
      });
    } catch (err) {
      await ctx.close();
      throw err;
    }

    var micNode = new AudioWorkletNode(ctx, 'gowa-mic');
    var speakerNode = new AudioWorkletNode(ctx, 'gowa-speaker');
    // A muted sink keeps the microphone worklet pulling input.
    var silent = ctx.createGain();
    silent.gain.value = 0;
    ctx.createMediaStreamSource(stream).connect(micNode);
    micNode.connect(silent);
    silent.connect(ctx.destination);
    speakerNode.connect(ctx.destination);

    var scheme = location.protocol === 'https:' ? 'wss://' : 'ws://';
    var query = '?call_id=' + encodeURIComponent(id) +
      '&device_id=' + encodeURIComponent(currentDevice());
    var ws = new WebSocket(scheme + location.host + base + '/call/audio' + query);
    ws.binaryType = 'arraybuffer';

    live = {ws: ws, ctx: ctx, stream: stream};

    ws.onopen = function () {
      liveButton.textContent = 'Stop audio';
      log('live audio connected');
    };
    ws.onmessage = function (event) {
      if (typeof event.data === 'string') {
        log('live: ' + event.data);
        return;
      }
      speakerNode.port.postMessage(new Int16Array(event.data));
    };
    ws.onclose = function () {
      if (live) stopLive();
      log('live audio closed');
    };
    micNode.port.onmessage = function (event) {
      if (ws.readyState === 1) ws.send(event.data.buffer);
    };
  }

  function stopLive() {
    if (!live) return;
    var current = live;
    live = null;
    liveButton.textContent = 'Live audio';
    try { current.ws.close(); } catch (err) {}
    try {
      current.stream.getTracks().forEach(function (track) { track.stop(); });
    } catch (err) {}
    try { current.ctx.close(); } catch (err) {}
  }

  // The dashboard websocket hub also carries CALL_INCOMING events. The panel
  // keeps one connection open so an inbound ring surfaces without polling.
  function handleIncomingMessage(raw) {
    var message;
    try {
      message = JSON.parse(raw);
    } catch (err) {
      return;
    }
    if (!message || !message.result) return;

    var incoming = message.result;
    if (message.code === 'CALL_ENDED') {
      if (incoming.call_id && incoming.call_id === callID.value.trim()) {
        banner.hidden = true;
        log('incoming call ended');
        setCallActive(false);
      }
      return;
    }
    if (message.code !== 'CALL_INCOMING') return;

    panel.hidden = false;
    setCallActive(true);
    banner.hidden = false;
    banner.textContent = 'Incoming call from ' + (incoming.from || 'unknown') + ' - click Answer';
    callID.value = incoming.call_id || '';
    if (incoming.device_id) device.value = incoming.device_id;
    log('incoming call from ' + (incoming.from || 'unknown') + ' (' + (incoming.call_id || '?') + ')');
  }

  function connectIncomingSocket() {
    if (incomingWS) return;
    var scheme = location.protocol === 'https:' ? 'wss://' : 'ws://';
    var ws = new WebSocket(scheme + location.host + base + '/ws');
    incomingWS = ws;
    ws.onmessage = function (event) {
      if (typeof event.data === 'string') handleIncomingMessage(event.data);
    };
    ws.onclose = function () {
      incomingWS = null;
      incomingRetry = setTimeout(connectIncomingSocket, 3000);
    };
  }

  toggle.addEventListener('click', function () {
    panel.hidden = !panel.hidden;
    if (!panel.hidden && !device.value) device.value = selectedDevice();
  });

  document.getElementById('gowa-beta-call-start').addEventListener('click', async function () {
    var body = {target: target.value.trim()};
    if (audio.value.trim()) body.audio_file_path = audio.value.trim();
    if (record.value.trim()) body.record_path = record.value.trim();
    try {
      var result = await post('start', body);
      var started = result && result.results && result.results.call_id;
      if (started) {
        callID.value = started;
        setCallActive(true);
      }
    } catch (err) {
      log('error: ' + err.message);
    }
  });

  document.getElementById('gowa-beta-call-answer').addEventListener('click', async function () {
    var body = {call_id: callID.value.trim()};
    if (record.value.trim()) body.record_path = record.value.trim();
    try {
      await post('answer', body);
      banner.hidden = true;
      setCallActive(true);
    } catch (err) {
      log('error: ' + err.message);
    }
  });

  document.getElementById('gowa-beta-call-hangup').addEventListener('click', async function () {
    try {
      await post('hangup', {call_id: callID.value.trim()});
      stopLive();
      banner.hidden = true;
      setCallActive(false);
    } catch (err) {
      log('error: ' + err.message);
    }
  });

  liveButton.addEventListener('click', async function () {
    if (live) {
      stopLive();
      return;
    }
    try {
      await startLive();
    } catch (err) {
      log('error: ' + err.message);
    }
  });

  connectIncomingSocket();
})();
</script>`
