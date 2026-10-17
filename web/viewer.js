// Viewer: draws the state stream from /ws and sends K / L as input.
// It holds no game logic; everything it shows comes from the API.
// Curves, scenery, fog, smoothing and the crash flash are cosmetic only.
(() => {
  const canvas = document.getElementById("screen");
  const ctx = canvas.getContext("2d");

  const CAR_LENGTH = 4;   // world units, matches the core's default
  const CAM_BACK = 10;    // camera distance behind the car's rear edge
  const NEAR = 8;         // depth that maps to the bottom of the screen
  const VIEW = 300;       // how far ahead the road is drawn
  const SEG = 10;         // length of one road stripe
  const TREE_EVERY = 30;
  const CURVE = 1.2e-3;
  const NEAREST = NEAR - CAR_LENGTH - CAM_BACK; // "ahead" value at the bottom edge
  const SANS = "system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif";
  const MONO = "ui-monospace, Menlo, monospace";

  const CAR_COLOURS = ["#2f6fdb", "#e8e8e8", "#f2b632", "#2aa876", "#8e5bd6", "#e2703a"];
  const PLAYER_COLOUR = "#d4202f";
  const GAUGE_MAX = 200;  // the speedometer's scale; speed itself has no limit
  const START_LINE = 12;  // road position of the start banner; every run starts at 0
  const START_ZONE = 90;  // the crowd stands up to here, and trees begin after it
  const ZONE = 600;       // length of a stretch of grass, beach or fields

  // Stretches of roadside, in order. The first is always grass.
  const ZONES = ["grass", "beach-left", "wheat", "beach-right", "grass", "lavender", "beach-left", "wheat", "grass", "beach-right"];
  const SPONSORS = [["TURBO", "#1f63c9"], ["SUNSET FM", "#e2306c"], ["PALM COLA", "#d4202f"],
    ["RAD OIL", "#222"], ["BLUE WAVE", "#2aa876"], ["NEON", "#8e5bd6"]];

  let state = null;
  let stateAt = 0;      // when the latest state arrived
  let connected = false;
  let ws = null;
  let shownLane = null; // eased towards state.lane
  let scenery = 0;      // horizontal scroll of the backdrop
  let lastNose = null;
  let crashAt = null;   // when the current run ended, for the flash and shake
  let crashRun = null;
  let lastFrame = null;
  let puffs = [];       // tyre smoke, in lane units next to the player's car
  const PUFF_LIFE = 0.6;  // seconds

  function connect() {
    ws = new WebSocket(`ws://${location.host}/ws`);
    ws.onopen = () => { connected = true; };
    ws.onmessage = (e) => {
      const msg = JSON.parse(e.data);
      if (msg.type === "state") { state = msg.state; stateAt = performance.now(); }
      else if (msg.type === "error") console.warn("server:", msg.error);
    };
    ws.onclose = () => { connected = false; setTimeout(connect, 1000); };
  }

  function send(msg) {
    if (connected) ws.send(JSON.stringify(msg));
  }

  addEventListener("keydown", (e) => {
    if (e.repeat || e.metaKey || e.ctrlKey || e.altKey) return; // one lane per key press
    const key = e.key.toLowerCase();
    const driving = state && state.controller === "human";
    if (key === "k" && driving) send({ type: "input", action: "left" });
    else if (key === "l" && driving) send({ type: "input", action: "right" });
    else if (key === "h" && state && !driving) send({ type: "claim", name: "human" });
    else if ((key === " " || key === "enter" || key === "r") && state && state.game_over) {
      e.preventDefault();
      send({ type: "reset" });
    }
  });

  function resize() {
    const dpr = window.devicePixelRatio || 1;
    canvas.width = Math.round(innerWidth * dpr);
    canvas.height = Math.round(innerHeight * dpr);
  }
  addEventListener("resize", resize);
  resize();

  // Road bend at world position z. Positive bends right. Stretches near
  // zero stay straight.
  function curvature(z) {
    const v = Math.sin(z / 260) + 0.6 * Math.sin(z / 113 + 1.3);
    return Math.sign(v) * Math.max(0, Math.abs(v) - 0.35) * CURVE;
  }

  const clamp01 = (v) => Math.max(0, Math.min(1, v));

  // ---------- colour ----------

  const rgbCache = new Map();
  function rgb(hex) {
    let c = rgbCache.get(hex);
    if (!c) {
      const n = parseInt(hex.slice(1), 16);
      c = [(n >> 16) & 255, (n >> 8) & 255, n & 255];
      rgbCache.set(hex, c);
    }
    return c;
  }

  // Blend two colours given as [r, g, b]; t = 0 gives a, t = 1 gives b.
  function mix(a, b, t) {
    return `rgb(${Math.round(a[0] + (b[0] - a[0]) * t)},${Math.round(a[1] + (b[1] - a[1]) * t)},${Math.round(a[2] + (b[2] - a[2]) * t)})`;
  }

  // Lighten (f > 0) or darken (f < 0) a hex colour.
  function shade(hex, f) {
    return mix(rgb(hex), f > 0 ? [255, 255, 255] : [0, 0, 0], Math.abs(f));
  }

  const FOG = rgb("#e4f1fb");
  const fogAt = (a) => 0.75 * Math.pow(clamp01((a - 30) / (VIEW - 30)), 1.3);

  const GRASS = [rgb("#5aa84a"), rgb("#4f9a42")];
  const RUMBLE = [rgb("#ececec"), rgb("#cc342b")];
  const TARMAC = [rgb("#5d6169"), rgb("#575b63")];
  const PAINT = rgb("#f2f2f2");
  const SAND = [rgb("#f1dca0"), rgb("#e7cf8e")];
  const FOAM = [rgb("#ffffff"), rgb("#dff4f2")];
  const SHALLOW = rgb("#46c9cc");
  const DEEP = rgb("#2b8ccc");
  const CROPS = { wheat: [rgb("#e2c050"), rgb("#cfa93a")], lavender: [rgb("#a68bd8"), rgb("#9277c6")] };

  // Deterministic noise in [0, 1) for cosmetic variety.
  const hash = (n) => { const v = Math.sin(n * 127.1 + 311.7) * 43758.5453; return v - Math.floor(v); };

  // The kind of roadside at road position z, and how far into the stretch
  // it is (0 at either end, 1 in the middle), so a beach can come and go gently.
  function zoneAt(z) {
    if (z < ZONE) return { kind: "grass", ramp: 1 };
    const i = Math.floor(z / ZONE), into = z - i * ZONE;
    return { kind: ZONES[i % ZONES.length], ramp: clamp01(into / 80) * clamp01((ZONE - into) / 80) };
  }

  // ---------- backdrop ----------

  function ridge(x, seed) {
    return 0.5 + 0.28 * Math.sin(x * 0.011 + seed) + 0.16 * Math.sin(x * 0.027 + seed * 2.1)
      + 0.06 * Math.sin(x * 0.071 + seed * 3.7);
  }

  function ridgePath(W, base, shift, height, seed, floor) {
    ctx.beginPath();
    ctx.moveTo(0, base);
    for (let x = 0; x <= W + 8; x += 8) {
      const r = Math.max(0, ridge(x + shift, seed) - floor) / (1 - floor);
      ctx.lineTo(x, base - height * r);
    }
    ctx.lineTo(W, base);
    ctx.closePath();
  }

  function drawCloud(x, y, r) {
    ctx.fillStyle = "rgba(255,255,255,0.92)";
    ctx.beginPath();
    ctx.ellipse(x, y, r * 1.6, r * 0.55, 0, 0, Math.PI * 2);
    ctx.ellipse(x - r * 0.7, y - r * 0.3, r * 0.8, r * 0.5, 0, 0, Math.PI * 2);
    ctx.ellipse(x + r * 0.5, y - r * 0.45, r * 0.9, r * 0.65, 0, 0, Math.PI * 2);
    ctx.fill();
    ctx.fillStyle = "rgba(160,190,225,0.35)";                 // shaded underside
    ctx.beginPath();
    ctx.ellipse(x + r * 0.1, y + r * 0.2, r * 1.45, r * 0.28, 0, 0, Math.PI * 2);
    ctx.fill();
  }

  function drawBackdrop(W, horizon, t) {
    const sky = ctx.createLinearGradient(0, 0, 0, horizon);
    sky.addColorStop(0, "#1f63c9");
    sky.addColorStop(0.55, "#74b9f0");
    sky.addColorStop(1, "#e4f1fb");
    ctx.fillStyle = sky;
    ctx.fillRect(0, 0, W, horizon + 1);

    const sunX = W * 0.72 - scenery * 0.05, sunY = horizon * 0.4;
    const glow = ctx.createRadialGradient(sunX, sunY, 0, sunX, sunY, horizon * 0.6);
    glow.addColorStop(0, "rgba(255,250,225,1)");
    glow.addColorStop(0.08, "rgba(255,246,205,0.85)");
    glow.addColorStop(0.25, "rgba(255,240,200,0.3)");
    glow.addColorStop(1, "rgba(255,240,200,0)");
    ctx.fillStyle = glow;
    ctx.fillRect(0, 0, W, horizon);

    const span = W + 400;
    for (let i = 0; i < 7; i++) {
      const drift = scenery * (0.1 + 0.03 * (i % 3)) + t * (4 + i % 3);
      const x = ((i * 397 - drift) % span + span) % span - 200;
      drawCloud(x, horizon * (0.14 + 0.1 * ((i * 7) % 5)), horizon * (0.045 + 0.014 * (i % 3)));
    }

    // Far mountains with snow on the peaks, hazy towards the base.
    const seaH = Math.max(6, horizon * 0.045);
    const farH = horizon * 0.34;
    const mBase = horizon - seaH + 2;
    ridgePath(W, mBase, scenery * 0.3, farH, 1.7, 0);
    const rock = ctx.createLinearGradient(0, mBase - farH, 0, mBase);
    rock.addColorStop(0, "#7f98bf");
    rock.addColorStop(1, "#c5d6ec");
    ctx.fillStyle = rock;
    ctx.fill();
    ctx.save();
    ctx.clip();
    const snow = ctx.createLinearGradient(0, mBase - farH, 0, mBase - farH * 0.7);
    snow.addColorStop(0, "rgba(255,255,255,0.95)");
    snow.addColorStop(1, "rgba(255,255,255,0)");
    ctx.fillStyle = snow;
    ctx.fillRect(0, mBase - farH, W, farH * 0.3);
    ctx.restore();

    // The sea along the horizon, with glints that drift with the scenery.
    const sea = ctx.createLinearGradient(0, horizon - seaH, 0, horizon);
    sea.addColorStop(0, "#3f8fd0");
    sea.addColorStop(1, "#7fc0ea");
    ctx.fillStyle = sea;
    ctx.fillRect(0, horizon - seaH, W, seaH + 1);
    for (let i = 0; i < 40; i++) {
      const x = ((i * 137.5 - scenery * 0.3 + t * 6 * (1 + i % 3)) % W + W) % W;
      const y = horizon - seaH * (0.15 + ((i * 0.618) % 1) * 0.75);
      const a = 0.35 + 0.35 * Math.sin(t * 2.5 + i * 1.7);
      if (a <= 0.05) continue;
      ctx.fillStyle = `rgba(255,255,255,${a.toFixed(2)})`;
      ctx.fillRect(x, y, 4 + (i % 4) * 3, 1);
    }

    // Green headlands in front of the sea, with gaps where the water shows.
    const nearH = horizon * 0.16;
    ridgePath(W, horizon + 1, scenery * 0.6 + 500, nearH, 4.2, 0.38);
    const hill = ctx.createLinearGradient(0, horizon - nearH, 0, horizon);
    hill.addColorStop(0, "#6aa65c");
    hill.addColorStop(1, "#3f7b3d");
    ctx.fillStyle = hill;
    ctx.fill();
  }

  // ---------- sprites, drawn in lane units with the origin on the ground ----------

  function shadow(w) {
    ctx.fillStyle = "rgba(0,0,0,0.28)";
    ctx.beginPath();
    ctx.ellipse(0, 0, w, w * 0.14, 0, 0, Math.PI * 2);
    ctx.fill();
  }

  // A soft glow; colour is "r,g,b".
  function lightGlow(x, y, r, colour, alpha) {
    const g = ctx.createRadialGradient(x, y, 0, x, y, r);
    g.addColorStop(0, `rgba(${colour},${alpha})`);
    g.addColorStop(1, `rgba(${colour},0)`);
    ctx.fillStyle = g;
    ctx.fillRect(x - r, y - r, r * 2, r * 2);
  }

  function bodyGradient(colour, top, bottom) {
    const g = ctx.createLinearGradient(0, top, 0, bottom);
    g.addColorStop(0, shade(colour, 0.3));
    g.addColorStop(0.4, colour);
    g.addColorStop(1, shade(colour, -0.4));
    return g;
  }

  function drawCar(colour) {
    const w = 0.62, hw = w / 2;
    shadow(hw * 1.2);

    ctx.fillStyle = "#0e0e10";                                // tyres
    ctx.beginPath();
    ctx.roundRect(-hw + 0.01, -0.13, 0.14, 0.13, 0.03);
    ctx.roundRect(hw - 0.15, -0.13, 0.14, 0.13, 0.03);
    ctx.fill();

    ctx.fillStyle = shade(colour, -0.15);                     // cabin
    ctx.beginPath();
    ctx.moveTo(-hw + 0.05, -0.29);
    ctx.lineTo(-hw + 0.13, -0.46);
    ctx.quadraticCurveTo(0, -0.48, hw - 0.13, -0.46);
    ctx.lineTo(hw - 0.05, -0.29);
    ctx.fill();

    const glass = ctx.createLinearGradient(0, -0.43, 0, -0.3);  // rear window
    glass.addColorStop(0, "#3b5573");
    glass.addColorStop(1, "#0d1520");
    ctx.fillStyle = glass;
    ctx.beginPath();
    ctx.moveTo(-hw + 0.1, -0.3);
    ctx.lineTo(-hw + 0.16, -0.43);
    ctx.lineTo(hw - 0.16, -0.43);
    ctx.lineTo(hw - 0.1, -0.3);
    ctx.fill();
    ctx.fillStyle = "rgba(255,255,255,0.22)";                 // reflection
    ctx.beginPath();
    ctx.moveTo(-hw + 0.13, -0.31);
    ctx.lineTo(-hw + 0.18, -0.42);
    ctx.lineTo(-0.04, -0.42);
    ctx.lineTo(-0.1, -0.31);
    ctx.fill();

    ctx.fillStyle = bodyGradient(colour, -0.31, -0.07);       // body
    ctx.beginPath();
    ctx.roundRect(-hw, -0.31, w, 0.24, 0.05);
    ctx.fill();
    ctx.fillStyle = "rgba(255,255,255,0.35)";                 // highlight along the boot lid
    ctx.fillRect(-hw + 0.05, -0.305, w - 0.1, 0.012);

    ctx.fillStyle = "#1a1a1c";                                // light bar
    ctx.beginPath();
    ctx.roundRect(-hw + 0.025, -0.275, w - 0.05, 0.065, 0.02);
    ctx.fill();
    ctx.fillStyle = "#c4161c";                                // tail lights
    ctx.fillRect(-hw + 0.04, -0.265, 0.13, 0.045);
    ctx.fillRect(hw - 0.17, -0.265, 0.13, 0.045);
    ctx.fillStyle = "rgba(255,170,160,0.6)";
    ctx.fillRect(-hw + 0.05, -0.262, 0.05, 0.015);
    ctx.fillRect(hw - 0.16, -0.262, 0.05, 0.015);

    ctx.fillStyle = "#ececec";                                // plate
    ctx.fillRect(-0.07, -0.19, 0.14, 0.05);
    ctx.fillStyle = "#222";                                   // bumper
    ctx.beginPath();
    ctx.roundRect(-hw, -0.1, w, 0.045, 0.015);
    ctx.fill();
    ctx.fillStyle = "rgba(255,255,255,0.12)";
    ctx.fillRect(-hw + 0.03, -0.1, w - 0.06, 0.01);
  }

  // The player's car: a low, wide convertible with a louvred rear and two people on board.
  function drawPlayerCar(crashed, now) {
    const w = 0.74, hw = w / 2;
    shadow(hw * 1.25);

    ctx.fillStyle = "#0e0e10";                                // wide tyres
    ctx.beginPath();
    ctx.roundRect(-hw, -0.14, 0.17, 0.14, 0.035);
    ctx.roundRect(hw - 0.17, -0.14, 0.17, 0.14, 0.035);
    ctx.fill();

    // Open top: the windscreen at the far end, then the two people in front of it.
    ctx.fillStyle = "rgba(175,215,240,0.45)";
    ctx.strokeStyle = "#2a2a2e";
    ctx.lineWidth = 0.018;
    ctx.beginPath();
    ctx.moveTo(-hw + 0.13, -0.29);
    ctx.lineTo(-hw + 0.17, -0.43);
    ctx.lineTo(hw - 0.17, -0.43);
    ctx.lineTo(hw - 0.13, -0.29);
    ctx.closePath();
    ctx.fill();
    ctx.stroke();

    ctx.fillStyle = "#f4f4f4";                                // shoulders: his white shirt, her pink top
    ctx.beginPath();
    ctx.roundRect(-0.245, -0.37, 0.16, 0.09, 0.045);
    ctx.fill();
    ctx.fillStyle = "#ff6fa8";
    ctx.beginPath();
    ctx.roundRect(0.085, -0.37, 0.16, 0.09, 0.045);
    ctx.fill();

    // Driver on the left, dark hair.
    ctx.fillStyle = "#2b1d14";
    ctx.beginPath();
    ctx.arc(-0.165, -0.42, 0.058, 0, Math.PI * 2);
    ctx.fill();
    // Passenger on the right, blond hair blowing out to the side.
    const t = now / 1000;
    ctx.fillStyle = "#f2cf6b";
    ctx.beginPath();
    ctx.moveTo(0.11, -0.47);
    for (let i = 0; i <= 6; i++) {
      const u = i / 6;
      ctx.lineTo(0.17 + u * 0.2, -0.47 + u * 0.07 + Math.sin(t * 9 - u * 5) * 0.02 * u);
    }
    for (let i = 6; i >= 0; i--) {
      const u = i / 6;
      ctx.lineTo(0.17 + u * 0.18, -0.4 + u * 0.05 + Math.sin(t * 9 - u * 5 + 0.6) * 0.02 * u);
    }
    ctx.lineTo(0.11, -0.37);
    ctx.fill();
    ctx.beginPath();
    ctx.arc(0.165, -0.42, 0.058, 0, Math.PI * 2);
    ctx.fill();

    ctx.fillStyle = bodyGradient(PLAYER_COLOUR, -0.3, -0.07); // body
    ctx.beginPath();
    ctx.roundRect(-hw, -0.29, w, 0.22, 0.06);
    ctx.fill();
    ctx.fillStyle = "rgba(255,255,255,0.4)";
    ctx.fillRect(-hw + 0.06, -0.285, w - 0.12, 0.012);
    ctx.fillStyle = shade(PLAYER_COLOUR, -0.3);               // mirrors
    ctx.beginPath();
    ctx.ellipse(-hw + 0.02, -0.31, 0.03, 0.02, 0, 0, Math.PI * 2);
    ctx.ellipse(hw - 0.02, -0.31, 0.03, 0.02, 0, 0, Math.PI * 2);
    ctx.fill();

    // Rear grille: lights behind black louvres.
    const lit = crashed && Math.floor(now / 250) % 2 === 0;
    ctx.fillStyle = "#121214";
    ctx.beginPath();
    ctx.roundRect(-hw + 0.04, -0.245, w - 0.08, 0.1, 0.02);
    ctx.fill();
    ctx.fillStyle = lit ? "#ff9a1f" : "#d0141c";
    ctx.fillRect(-hw + 0.06, -0.235, 0.17, 0.08);
    ctx.fillRect(hw - 0.23, -0.235, 0.17, 0.08);
    ctx.fillStyle = shade(PLAYER_COLOUR, -0.25);
    for (let i = 0; i < 4; i++) ctx.fillRect(-hw + 0.04, -0.232 + i * 0.024, w - 0.08, 0.008);
    if (lit) {
      lightGlow(-hw + 0.14, -0.195, 0.28, "255,170,40", 0.6);
      lightGlow(hw - 0.14, -0.195, 0.28, "255,170,40", 0.6);
    }

    ctx.fillStyle = "#ececec";                                // plate
    ctx.fillRect(-0.07, -0.135, 0.14, 0.045);
    ctx.fillStyle = "#1c1c1e";                                // diffuser
    ctx.beginPath();
    ctx.roundRect(-hw + 0.02, -0.085, w - 0.04, 0.04, 0.015);
    ctx.fill();
    for (const x of [-0.2, -0.14, 0.14, 0.2]) {                // chrome exhausts
      ctx.fillStyle = "#d8dce2";
      ctx.beginPath();
      ctx.arc(x, -0.065, 0.026, 0, Math.PI * 2);
      ctx.fill();
      ctx.fillStyle = "#26282c";
      ctx.beginPath();
      ctx.arc(x, -0.065, 0.016, 0, Math.PI * 2);
      ctx.fill();
    }
  }

  function drawBarrier(now) {
    const w = 0.7, hw = w / 2;
    shadow(hw * 1.1);
    ctx.fillStyle = "#3c3c40";                                // legs and feet
    ctx.fillRect(-hw + 0.05, -0.3, 0.05, 0.3);
    ctx.fillRect(hw - 0.1, -0.3, 0.05, 0.3);
    ctx.fillRect(-hw + 0.01, -0.025, 0.13, 0.025);
    ctx.fillRect(hw - 0.14, -0.025, 0.13, 0.025);

    for (const top of [-0.44, -0.22]) {                       // two striped boards
      ctx.save();
      ctx.beginPath();
      ctx.roundRect(-hw, top, w, 0.15, 0.02);
      ctx.clip();
      ctx.fillStyle = "#f6f6f6";
      ctx.fillRect(-hw, top, w, 0.15);
      ctx.fillStyle = "#e8452c";
      for (let x = -hw - 0.2; x < hw; x += 0.2) {
        ctx.beginPath();
        ctx.moveTo(x, top + 0.15);
        ctx.lineTo(x + 0.15, top);
        ctx.lineTo(x + 0.25, top);
        ctx.lineTo(x + 0.1, top + 0.15);
        ctx.fill();
      }
      ctx.fillStyle = "rgba(0,0,0,0.18)";                     // lower edge in shade
      ctx.fillRect(-hw, top + 0.115, w, 0.035);
      ctx.restore();
    }

    const on = Math.floor(now / 400) % 2 === 0;               // blinking warning lamps
    for (const x of [-hw + 0.075, hw - 0.075]) {
      if (on) lightGlow(x, -0.49, 0.18, "255,190,40", 0.7);
      ctx.fillStyle = on ? "#ffd84a" : "#b07a00";
      ctx.beginPath();
      ctx.arc(x, -0.49, 0.04, 0, Math.PI * 2);
      ctx.fill();
    }
  }

  function drawPalm(lean) {
    shadow(0.35);
    // Trunk: tapered, with rings, along a gentle curve.
    const tx = lean * 0.35, ty = -2.2, cx = lean * 0.1, cy = -1.2;
    const at = (u) => [2 * (1 - u) * u * cx + u * u * tx, 2 * (1 - u) * u * cy + u * u * ty];
    ctx.lineCap = "round";
    const steps = 10;
    for (let k = 0; k < steps; k++) {
      const [x1, y1] = at(k / steps), [x2, y2] = at((k + 1) / steps);
      ctx.strokeStyle = k % 2 ? "#7a512e" : "#8e6239";
      ctx.lineWidth = 0.17 - 0.07 * (k / steps);
      ctx.beginPath();
      ctx.moveTo(x1, y1);
      ctx.lineTo(x2, y2);
      ctx.stroke();
    }

    ctx.translate(tx, ty);
    const frond = (dir, len, droop, colour) => {
      const ex = dir * len, ey = 0.2 + Math.abs(dir) * droop;
      const mx = dir * len * 0.55, my = -0.55 + Math.abs(dir) * 0.3;
      ctx.fillStyle = colour;
      ctx.beginPath();
      ctx.moveTo(0, 0);
      ctx.quadraticCurveTo(mx, my - 0.1, ex, ey);
      ctx.quadraticCurveTo(mx, my + 0.08, 0, 0.04);
      ctx.fill();
    };
    for (let i = 0; i < 6; i++) frond(-1 + i / 2.5, 0.95, 0.15, "#1d5e2a");  // back layer
    for (let i = 0; i < 7; i++) frond(-1 + i / 3, 1.1, 0.1, i % 2 ? "#2f8f3e" : "#267a34");
    ctx.fillStyle = "#5a3a1c";                                // coconuts
    ctx.beginPath();
    ctx.arc(-0.07, 0.06, 0.055, 0, Math.PI * 2);
    ctx.arc(0.06, 0.07, 0.055, 0, Math.PI * 2);
    ctx.arc(0, 0.12, 0.05, 0, Math.PI * 2);
    ctx.fill();
  }

  function drawBush(flowers) {
    shadow(0.42);
    ctx.fillStyle = "#24683a";
    ctx.beginPath();
    ctx.arc(-0.22, -0.18, 0.24, 0, Math.PI * 2);
    ctx.arc(0.2, -0.17, 0.22, 0, Math.PI * 2);
    ctx.arc(0, -0.34, 0.27, 0, Math.PI * 2);
    ctx.fill();
    ctx.fillStyle = "#32874a";
    ctx.beginPath();
    ctx.arc(-0.12, -0.3, 0.16, 0, Math.PI * 2);
    ctx.arc(0.12, -0.28, 0.14, 0, Math.PI * 2);
    ctx.fill();
    ctx.fillStyle = "#4fae5e";
    ctx.beginPath();
    ctx.arc(-0.06, -0.44, 0.1, 0, Math.PI * 2);
    ctx.fill();
    if (flowers) {
      ctx.fillStyle = flowers;
      for (const [x, y] of [[-0.25, -0.25], [0.05, -0.5], [0.22, -0.22], [-0.05, -0.2], [0.15, -0.38]]) {
        ctx.beginPath();
        ctx.arc(x, y, 0.035, 0, Math.PI * 2);
        ctx.fill();
      }
    }
  }

  function drawBillboard(side) {
    shadow(0.8);
    ctx.fillStyle = "#555a60";                                // posts
    ctx.fillRect(-0.55, -1.0, 0.07, 1.0);
    ctx.fillRect(0.48, -1.0, 0.07, 1.0);
    const g = ctx.createLinearGradient(-0.8, -1.75, 0.8, -0.95);
    g.addColorStop(0, "#ff7a2f");
    g.addColorStop(1, "#e2306c");
    ctx.fillStyle = "#f4f4f4";                                // frame
    ctx.beginPath();
    ctx.roundRect(-0.84, -1.79, 1.68, 0.88, 0.06);
    ctx.fill();
    ctx.fillStyle = g;
    ctx.beginPath();
    ctx.roundRect(-0.79, -1.74, 1.58, 0.78, 0.04);
    ctx.fill();
    ctx.save();                                               // text in pixel-sized units
    ctx.scale(0.01, 0.01);
    ctx.fillStyle = "#fff";
    ctx.textAlign = "center";
    ctx.textBaseline = "middle";
    ctx.font = `italic 900 34px ${SANS}`;
    ctx.fillText("OUTRUN", 0, -142);
    ctx.font = `600 11px ${SANS}`;
    ctx.fillText(side < 0 ? "STAY IN YOUR LANE" : "30 TICKS PER SECOND", 0, -112);
    ctx.restore();
  }

  function drawUmbrella(seed) {
    shadow(0.45);
    ctx.fillStyle = "#e8e8e8";
    ctx.fillRect(-0.02, -0.75, 0.04, 0.75);
    const colours = seed > 0.5 ? ["#e2306c", "#fff"] : ["#1f9bd6", "#fff"];
    for (let i = 0; i < 6; i++) {
      ctx.fillStyle = colours[i % 2];
      ctx.beginPath();
      ctx.moveTo(0, -0.95);
      ctx.lineTo(-0.5 + i / 6, -0.72);
      ctx.lineTo(-0.5 + (i + 1) / 6, -0.72);
      ctx.fill();
    }
    ctx.fillStyle = "#f6f0e0";                                // towel
    ctx.fillRect(0.12, -0.03, 0.32, 0.05);
  }

  // A person seen from behind, about 0.55 lane widths tall.
  function drawPerson(seed, now, cheer) {
    const shirts = ["#d4202f", "#1f63c9", "#f2b632", "#2aa876", "#ffffff", "#e2306c", "#8e5bd6"];
    const hairs = ["#2b1d14", "#5a3a1c", "#f2cf6b", "#1a1a1a", "#a0522d"];
    const shirt = shirts[Math.floor(seed * shirts.length)];
    const hair = hairs[Math.floor(hash(seed * 91) * hairs.length)];
    const h = 0.9 + 0.2 * hash(seed * 17);
    ctx.save();
    ctx.scale(h, h);
    ctx.fillStyle = "#2c3e5c";                                // legs
    ctx.fillRect(-0.07, -0.22, 0.06, 0.22);
    ctx.fillRect(0.01, -0.22, 0.06, 0.22);
    if (cheer) {                                              // arms up, waving
      const wave = Math.sin(now / 160 + seed * 20) * 0.05;
      ctx.strokeStyle = "#e9b48a";
      ctx.lineWidth = 0.035;
      ctx.lineCap = "round";
      ctx.beginPath();
      ctx.moveTo(-0.08, -0.4);
      ctx.lineTo(-0.15 + wave, -0.55);
      ctx.moveTo(0.08, -0.4);
      ctx.lineTo(0.15 - wave, -0.55);
      ctx.stroke();
    }
    ctx.fillStyle = shirt;                                    // body
    ctx.beginPath();
    ctx.roundRect(-0.1, -0.44, 0.2, 0.24, 0.05);
    ctx.fill();
    ctx.fillStyle = hair;                                     // head from behind
    ctx.beginPath();
    ctx.arc(0, -0.5, 0.065, 0, Math.PI * 2);
    ctx.fill();
    ctx.restore();
  }

  // Three people standing together; the crowd is built from these.
  function drawCrowd(seed, now) {
    for (let k = 0; k < 3; k++) {
      const s = hash(seed * 7 + k * 13);
      ctx.save();
      ctx.translate(-0.42 + k * 0.42 + (s - 0.5) * 0.12, (hash(seed + k) - 0.5) * 0.04);
      drawPerson(s, now, s > 0.45);
      ctx.restore();
    }
  }

  function drawFlagger(now) {
    drawPerson(0.3, now, false);
    const swing = Math.sin(now / 180) * 0.5;                  // the flag, waved overhead
    ctx.save();
    ctx.translate(0.08, -0.42);
    ctx.rotate(-0.4 + swing);
    ctx.strokeStyle = "#ddd";
    ctx.lineWidth = 0.025;
    ctx.beginPath();
    ctx.moveTo(0, 0);
    ctx.lineTo(0, -0.42);
    ctx.stroke();
    const sq = 0.06;
    for (let i = 0; i < 4; i++) {
      for (let j = 0; j < 3; j++) {
        ctx.fillStyle = (i + j) % 2 ? "#111" : "#fff";
        ctx.fillRect(i * sq, -0.42 + j * sq, sq, sq);
      }
    }
    ctx.restore();
  }

  function drawStartGantry(half, now) {
    const x = half + 0.45;
    for (const side of [-1, 1]) {                             // poles
      ctx.fillStyle = "#8d939b";
      ctx.fillRect(side * x - 0.06, -2.5, 0.12, 2.5);
      ctx.fillStyle = "rgba(255,255,255,0.35)";
      ctx.fillRect(side * x - 0.06, -2.5, 0.03, 2.5);
    }
    ctx.fillStyle = "#b3161a";                                // banner
    ctx.beginPath();
    ctx.roundRect(-x - 0.1, -2.5, 2 * x + 0.2, 0.66, 0.05);
    ctx.fill();
    ctx.fillStyle = "#fbfbf7";
    ctx.beginPath();
    ctx.roundRect(-x - 0.05, -2.45, 2 * x + 0.1, 0.56, 0.04);
    ctx.fill();
    const sq = 0.093;                                         // chequered ends
    for (const side of [-1, 1]) {
      for (let i = 0; i < 4; i++) {
        for (let j = 0; j < 6; j++) {
          if ((i + j) % 2) continue;
          ctx.fillStyle = "#111";
          ctx.fillRect(side * (x - 0.2) + (i - 2) * sq / 2, -2.45 + j * sq, sq / 2, sq);
        }
      }
    }
    ctx.save();
    ctx.scale(0.01, 0.01);
    ctx.fillStyle = "#c8202a";
    ctx.textAlign = "center";
    ctx.textBaseline = "middle";
    ctx.font = `italic 900 46px ${SANS}`;
    ctx.letterSpacing = "8px";
    ctx.fillText("START", 0, -215);
    ctx.letterSpacing = "0px";
    ctx.restore();

    // Start lights on the left pole: the green one is lit.
    ctx.fillStyle = "#1b1d22";
    ctx.beginPath();
    ctx.roundRect(-x - 0.13, -1.75, 0.26, 0.74, 0.05);
    ctx.fill();
    [["#4a0d0d", 0], ["#4a0d0d", 0], ["#3ddc84", 1]].forEach(([c, lit], i) => {
      const y = -1.62 + i * 0.24;
      if (lit) lightGlow(-x, y, 0.3, "61,220,132", 0.55 + 0.15 * Math.sin(now / 200));
      ctx.fillStyle = c;
      ctx.beginPath();
      ctx.arc(-x, y, 0.08, 0, Math.PI * 2);
      ctx.fill();
    });
  }

  // A wall of made-up sponsor boards beside the start line.
  function drawSponsors(side) {
    shadow(0.9);
    ctx.fillStyle = "#6b7078";
    ctx.fillRect(-0.62, -1.75, 0.05, 1.75);
    ctx.fillRect(0.57, -1.75, 0.05, 1.75);
    ctx.save();
    ctx.scale(0.01, 0.01);
    ctx.textAlign = "center";
    ctx.textBaseline = "middle";
    for (let r = 0; r < 3; r++) {
      for (let c = 0; c < 2; c++) {
        const [name, colour] = SPONSORS[(r * 2 + c + (side > 0 ? 3 : 0)) % SPONSORS.length];
        const x = -60 + c * 60, y = -170 + r * 40;
        ctx.fillStyle = "#fff";
        ctx.fillRect(x, y, 58, 36);
        ctx.fillStyle = colour;
        ctx.fillRect(x + 3, y + 3, 52, 30);
        ctx.fillStyle = "#fff";
        ctx.font = `italic 900 13px ${SANS}`;
        const k = Math.min(1, 46 / ctx.measureText(name).width);  // shrink long names to fit
        ctx.save();
        ctx.translate(x + 29, y + 18);
        ctx.scale(k, k);
        ctx.fillText(name, 0, 0);
        ctx.restore();
      }
    }
    ctx.restore();
  }

  function drawPuff(p) {
    const life = p.age / PUFF_LIFE;
    const r = p.size * (1 + life * 1.6);
    const g = ctx.createRadialGradient(0, -r, 0, 0, -r, r);
    g.addColorStop(0, "rgba(255,255,255,0.7)");
    g.addColorStop(1, "rgba(255,255,255,0)");
    ctx.globalAlpha *= 0.8 * (1 - life);
    ctx.fillStyle = g;
    ctx.fillRect(-r, -2 * r, 2 * r, 2 * r);
  }

  // Sand, surf and sea, or crop fields, between the rumble strip and the edge
  // of the screen. Drawn over the grass of one stripe.
  function drawRoadside(p, q, zone, odd, f) {
    const edge = state.lanes / 2 + 0.28;
    const strip = (x1, x2, colour) => {
      ctx.fillStyle = colour;
      ctx.beginPath();
      ctx.moveTo(p.cx + x1 * p.unit, p.y + 0.5);
      ctx.lineTo(p.cx + x2 * p.unit, p.y + 0.5);
      ctx.lineTo(q.cx + x2 * q.unit, q.y);
      ctx.lineTo(q.cx + x1 * q.unit, q.y);
      ctx.fill();
    };
    if (zone.kind === "beach-left" || zone.kind === "beach-right") {
      const s = zone.kind === "beach-left" ? -1 : 1;
      const sand = 0.4 + 2.6 * zone.ramp;
      const shore = edge + sand + 0.3 + (1 - zone.ramp) * 30; // the sea comes in from far away
      strip(s * edge, s * (edge + sand), mix(SAND[odd], FOG, f));
      if (zone.ramp > 0.3) strip(s * (edge + sand), s * (shore - 0.3), mix(SAND[1 - odd], FOG, f));
      strip(s * (shore - 0.3), s * shore, mix(FOAM[odd], FOG, f));
      strip(s * shore, s * (shore + 1.2), mix(SHALLOW, FOG, f));
      strip(s * (shore + 1.2), s * 200, mix(DEEP, FOG, f));
    } else if (CROPS[zone.kind]) {
      const verge = edge + 0.6 + (1 - zone.ramp) * 30;
      const colour = mix(CROPS[zone.kind][odd], FOG, f);
      strip(-verge, -200, colour);
      strip(verge, 200, colour);
      const row = mix(CROPS[zone.kind][1 - odd], FOG, f);    // rows running alongside the road
      for (let k = 1; k <= 10; k++) {
        const x = verge + k * 0.7;
        strip(-x, -x - 0.22, row);
        strip(x, x + 0.22, row);
      }
    }
  }

  // ---------- frame ----------

  function draw(now) {
    requestAnimationFrame(draw);
    const dpr = window.devicePixelRatio || 1;
    const W = innerWidth, H = innerHeight;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);

    if (!state) {
      drawWaiting(W, H, now);
      return;
    }

    if (state.game_over && crashRun !== state.run_id) { crashAt = now; crashRun = state.run_id; }
    const crashAge = state.game_over && crashAt !== null ? (now - crashAt) / 1000 : Infinity;

    // A short shake when the car hits something.
    if (crashAge < 0.35) {
      const k = 7 * (1 - crashAge / 0.35);
      ctx.setTransform(dpr, 0, 0, dpr, (Math.random() - 0.5) * k * dpr, (Math.random() - 0.5) * k * dpr);
    }

    // Glide between states. A tick lasts 1 / (tick_rate * pace) seconds on
    // the wall clock, and game time runs at pace times wall time.
    const pace = state.pace || 1;
    const tickSeconds = 1 / (state.tick_rate * pace);
    const since = state.game_over ? 0 : Math.min((now - stateAt) / 1000, 1.5 * tickSeconds) * pace;
    const nose = state.distance + CAR_LENGTH + since * state.speed;

    const lanes = state.lanes;
    const horizon = H * 0.4;
    const roadH = H - horizon;
    const lanePx = Math.min(W * 0.9, H * 1.5) / lanes; // lane width at the bottom edge

    if (lastNose !== null && nose >= lastNose) scenery += curvature(nose) * (nose - lastNose) * W * 3;
    lastNose = nose;

    drawBackdrop(W, horizon, now / 1000);

    // Road outline: one point per stripe boundary, bending away with distance.
    const pts = [];
    {
      let a = NEAREST, bend = 0, off = 0;
      let next = (Math.floor((nose + a) / SEG) + 1) * SEG - nose;
      const add = () => {
        const s = NEAR / (a + CAR_LENGTH + CAM_BACK);
        pts.push({ a, off, s, y: horizon + roadH * s, cx: W / 2 + off * lanePx * s, unit: lanePx * s });
      };
      add();
      while (a < VIEW) {
        const na = Math.min(next, VIEW);
        const len = na - a;
        bend += curvature(nose + (a + na) / 2) * len;
        off += bend * len;
        a = na;
        next += SEG;
        add();
      }
    }

    // Sideways shift of the road at a given distance.
    const offsetAt = (a) => {
      for (let i = 1; i < pts.length; i++) {
        if (a <= pts[i].a) {
          const p = pts[i - 1], q = pts[i];
          return p.off + (q.off - p.off) * (a - p.a) / (q.a - p.a);
        }
      }
      return pts[pts.length - 1].off;
    };

    const quad = (p, q, x1, x2, colour) => {
      ctx.fillStyle = colour;
      ctx.beginPath();
      ctx.moveTo(p.cx + x1 * p.unit, p.y + 0.5);
      ctx.lineTo(p.cx + x2 * p.unit, p.y + 0.5);
      ctx.lineTo(q.cx + x2 * q.unit, q.y);
      ctx.lineTo(q.cx + x1 * q.unit, q.y);
      ctx.fill();
    };

    ctx.fillStyle = mix(GRASS[0], FOG, fogAt(VIEW));
    ctx.fillRect(0, horizon, W, roadH);

    // Each stripe fades towards the colour of the sky at the horizon.
    const half = lanes / 2;
    for (let i = pts.length - 2; i >= 0; i--) {
      const p = pts[i], q = pts[i + 1];
      const odd = Math.floor((nose + (p.a + q.a) / 2) / SEG) % 2 === 0 ? 0 : 1;
      const f = fogAt((p.a + q.a) / 2);

      ctx.fillStyle = mix(GRASS[odd], FOG, f);
      ctx.fillRect(0, q.y, W, p.y - q.y + 1);
      drawRoadside(p, q, zoneAt(nose + (p.a + q.a) / 2), odd, f);

      quad(p, q, -half - 0.28, half + 0.28, mix(RUMBLE[odd], FOG, f));
      quad(p, q, -half, half, mix(TARMAC[odd], FOG, f));
      const paint = mix(PAINT, FOG, f);
      quad(p, q, -half + 0.03, -half + 0.1, paint);           // edge lines
      quad(p, q, half - 0.1, half - 0.03, paint);
      if (!odd) {
        for (let b = 1; b < lanes; b++) quad(p, q, b - half - 0.035, b - half + 0.035, paint);
      }
    }

    // Everything standing on the ground, far to near.
    const sprites = [];
    const first = Math.max(START_ZONE, Math.ceil((nose + NEAREST + 2) / TREE_EVERY) * TREE_EVERY);
    for (let z = Math.ceil(first / TREE_EVERY) * TREE_EVERY; z - nose < VIEW; z += TREE_EVERY) {
      const n = Math.round(z / TREE_EVERY);
      const zone = zoneAt(z);
      for (const side of [-1, 1]) {
        let x, paint;
        const beach = zone.kind === (side < 0 ? "beach-left" : "beach-right");
        if (beach) {
          if (n % 2 && zone.ramp > 0.5) {                     // palms on the sand, umbrellas further out
            sprites.push({ a: z - nose + 9, x: side * (half + 2.0), paint: () => drawUmbrella(hash(n)) });
          }
          x = half + 1.0;
          paint = () => drawPalm(-side);
        } else if (zone.kind === "wheat" || zone.kind === "lavender") {
          if (n % 3) continue;                                // open fields, a bush here and there
          x = half + 0.8;
          paint = () => drawBush(null);
        } else if (n % 9 === (side < 0 ? 4 : 8)) {
          x = half + 2.1;
          paint = () => drawBillboard(side);
        } else if (n % 3 !== 0) {
          x = half + 1.0;
          paint = () => drawPalm(-side);
        } else {
          x = half + 1.5;
          const flowers = n % 2 ? "#ff6fa8" : (n % 5 === 0 ? "#ffd84a" : null);
          paint = () => drawBush(flowers);
        }
        sprites.push({ a: z - nose, x: side * x, paint });
      }
    }
    for (const o of state.obstacles) {
      // Keep it on screen for exactly as long as the core can still collide with it.
      if (o.ahead > VIEW || o.ahead + o.length <= -CAR_LENGTH) continue;
      const a = Math.max(o.ahead - since * (state.speed - o.speed), NEAREST + 0.2);
      sprites.push({
        a,
        x: o.lane - (lanes - 1) / 2,
        paint: o.type === "car" ? () => drawCar(CAR_COLOURS[o.id % CAR_COLOURS.length]) : () => drawBarrier(now),
      });
    }

    // The start line: banner, crowd, flag and sponsor boards, left behind as the run begins.
    if (nose - START_ZONE < 20) {
      const show = (a) => a > NEAREST + 0.3 && a < VIEW;
      if (show(START_LINE - nose)) sprites.push({ a: START_LINE - nose, x: 0, paint: () => drawStartGantry(half, now) });
      if (show(START_LINE - 3 - nose)) sprites.push({ a: START_LINE - 3 - nose, x: -(half + 0.55), paint: () => drawFlagger(now) });
      for (const side of [-1, 1]) {
        if (show(START_LINE + 8 - nose)) sprites.push({ a: START_LINE + 8 - nose, x: side * (half + 2.7), paint: () => drawSponsors(side) });
        for (let z = -10; z < START_ZONE; z += 2.5) {
          if (!show(z - nose)) continue;
          const seed = z * 3.1 + side * 7.7;
          sprites.push({ a: z - nose, x: side * (half + 1.35), paint: () => drawCrowd(seed, now) });
        }
      }
    }

    if (shownLane === null) shownLane = state.lane;
    const slide = state.lane - shownLane;
    shownLane += slide * 0.3;
    const carX = shownLane - (lanes - 1) / 2;

    // Tyre smoke while changing lane, and a burst on a crash.
    const dt = lastFrame === null ? 0 : Math.min((now - lastFrame) / 1000, 0.1);
    lastFrame = now;
    for (const p of puffs) p.age += dt;
    puffs = puffs.filter((p) => p.age < PUFF_LIFE);
    const spawn = (n) => {
      for (let i = 0; i < n; i++) {
        puffs.push({ x: carX + (Math.random() < 0.5 ? -0.28 : 0.28), drift: (Math.random() - 0.5) * 0.6,
          size: 0.07 + Math.random() * 0.05, age: Math.random() * 0.1 });
      }
    };
    if (!state.game_over && Math.abs(slide) > 0.15 && Math.random() < dt * 25) spawn(1);
    if (crashAge < 0.05 && puffs.length < 8) spawn(10);
    for (const p of puffs) {
      sprites.push({ a: Math.max(-CAR_LENGTH - 0.3 - p.age * 2.5, NEAREST + 0.05), x: p.x + p.drift * p.age, paint: () => drawPuff(p) });
    }
    sprites.push({
      a: -CAR_LENGTH,
      x: carX,
      tilt: state.game_over ? 0.12 : slide * 0.1,
      paint: () => drawPlayerCar(state.game_over, now),
    });

    sprites.sort((s1, s2) => s2.a - s1.a);
    for (const sp of sprites) {
      const s = NEAR / (sp.a + CAR_LENGTH + CAM_BACK);
      const unit = lanePx * s;
      ctx.save();
      // Far sprites thin out into the haze like the road; near ones stay solid.
      ctx.globalAlpha = Math.min(clamp01((VIEW - sp.a) / 60), 1 - 0.6 * fogAt(sp.a));
      ctx.translate(W / 2 + (sp.x + offsetAt(sp.a)) * unit, horizon + roadH * s);
      ctx.scale(unit, unit);
      if (sp.tilt) ctx.rotate(sp.tilt);
      sp.paint();
      ctx.restore();
    }

    // Darker corners, a little more as the car gets faster.
    const v = ctx.createRadialGradient(W / 2, H * 0.55, Math.min(W, H) * 0.35, W / 2, H * 0.55, Math.max(W, H) * 0.8);
    v.addColorStop(0, "rgba(0,0,0,0)");
    v.addColorStop(1, `rgba(0,0,0,${(0.22 + 0.18 * clamp01(state.speed / GAUGE_MAX)).toFixed(3)})`);
    ctx.fillStyle = v;
    ctx.fillRect(0, 0, W, H);

    if (crashAge < 0.5) {
      ctx.fillStyle = `rgba(255,255,255,${(0.75 * (1 - crashAge / 0.5)).toFixed(3)})`;
      ctx.fillRect(0, 0, W, H);
    }

    // The HUD is laid out for 440 px and scaled down on narrower screens.
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    const k = Math.min(1, W / 440);
    ctx.scale(k, k);
    drawHud(W / k, H / k, crashAge);
  }

  function drawWaiting(W, H, now) {
    const g = ctx.createLinearGradient(0, 0, 0, H);
    g.addColorStop(0, "#1f63c9");
    g.addColorStop(1, "#0b2147");
    ctx.fillStyle = g;
    ctx.fillRect(0, 0, W, H);
    ctx.fillStyle = "#fff";
    ctx.textAlign = "center";
    ctx.textBaseline = "middle";
    ctx.font = `italic 900 44px ${SANS}`;
    ctx.fillText("OUTRUN", W / 2, H / 2 - 20);
    ctx.font = `500 15px ${SANS}`;
    ctx.fillStyle = "rgba(255,255,255,0.75)";
    ctx.fillText("connecting" + ".".repeat(1 + Math.floor(now / 400) % 3), W / 2, H / 2 + 24);
  }

  // ---------- HUD ----------

  function panel(x, y, w, h) {
    ctx.fillStyle = "rgba(10,20,42,0.62)";
    ctx.beginPath();
    ctx.roundRect(x, y, w, h, 12);
    ctx.fill();
    ctx.strokeStyle = "rgba(255,255,255,0.16)";
    ctx.lineWidth = 1;
    ctx.stroke();
  }

  function label(text, x, y, align) {
    ctx.font = `700 11px ${SANS}`;
    ctx.fillStyle = "rgba(255,255,255,0.62)";
    ctx.textAlign = align || "left";
    ctx.letterSpacing = "1.5px";
    ctx.fillText(text, x, y);
    ctx.letterSpacing = "0px";
  }

  // Cut text to fit maxW, with an ellipsis.
  function fit(text, maxW) {
    if (ctx.measureText(text).width <= maxW) return text;
    while (text.length > 1 && ctx.measureText(text + "…").width > maxW) text = text.slice(0, -1);
    return text + "…";
  }

  function keycap(key, x, y) {
    ctx.font = `700 13px ${SANS}`;
    const w = Math.max(26, ctx.measureText(key).width + 16);
    ctx.fillStyle = "rgba(0,0,0,0.35)";
    ctx.beginPath();
    ctx.roundRect(x, y + 2, w, 26, 6);
    ctx.fill();
    ctx.fillStyle = "#f4f6fa";
    ctx.beginPath();
    ctx.roundRect(x, y, w, 26, 6);
    ctx.fill();
    ctx.fillStyle = "#18202c";
    ctx.textAlign = "center";
    ctx.fillText(key, x + w / 2, y + 13);
    return w;
  }

  // Keycaps with captions, centred on cx.
  function hints(items, cx, y) {
    ctx.textBaseline = "middle";
    ctx.font = `700 13px ${SANS}`;
    const widths = items.map(([key, text]) => {
      const kw = Math.max(26, ctx.measureText(key).width + 16);
      ctx.font = `600 13px ${SANS}`;
      const tw = ctx.measureText(text).width;
      ctx.font = `700 13px ${SANS}`;
      return kw + 8 + tw;
    });
    let x = cx - (widths.reduce((s, w) => s + w, 0) + 22 * (items.length - 1)) / 2;
    items.forEach(([key, text], i) => {
      const kw = keycap(key, x, y);
      ctx.font = `600 13px ${SANS}`;
      ctx.fillStyle = "#fff";
      ctx.textAlign = "left";
      ctx.fillText(text, x + kw + 8, y + 13);
      x += widths[i] + 22;
    });
  }

  function drawGauge(cx, cy, r) {
    const start = Math.PI * 0.75, sweep = Math.PI * 1.5;
    const frac = clamp01(state.speed / GAUGE_MAX);
    ctx.lineCap = "round";
    ctx.lineWidth = 8;
    ctx.strokeStyle = "rgba(255,255,255,0.14)";
    ctx.beginPath();
    ctx.arc(cx, cy, r, start, start + sweep);
    ctx.stroke();
    const g = ctx.createLinearGradient(cx - r, 0, cx + r, 0);
    g.addColorStop(0, "#3ddc84");
    g.addColorStop(0.55, "#ffd23f");
    g.addColorStop(1, "#ff4d4d");
    ctx.strokeStyle = g;
    ctx.beginPath();
    ctx.arc(cx, cy, r, start, start + sweep * Math.max(frac, 0.005));
    ctx.stroke();

    ctx.lineCap = "butt";
    ctx.lineWidth = 2;
    ctx.strokeStyle = "rgba(255,255,255,0.5)";
    for (let s = 0; s <= GAUGE_MAX; s += 25) {
      const ang = start + sweep * s / GAUGE_MAX;
      const r1 = r - 9, r2 = r - (s % 50 ? 13 : 17);
      ctx.beginPath();
      ctx.moveTo(cx + Math.cos(ang) * r1, cy + Math.sin(ang) * r1);
      ctx.lineTo(cx + Math.cos(ang) * r2, cy + Math.sin(ang) * r2);
      ctx.stroke();
    }

    ctx.textAlign = "center";
    ctx.textBaseline = "middle";
    ctx.fillStyle = "#fff";
    ctx.font = `italic 800 30px ${SANS}`;
    ctx.fillText(String(Math.round(state.speed)), cx, cy + 2);
    label("SPEED", cx, cy + 30, "center");
  }

  function drawHud(W, H, crashAge) {
    ctx.textBaseline = "alphabetic";

    // Score, top left.
    panel(16, 16, 176, 86);
    label("SCORE", 30, 36);
    ctx.font = `italic 800 30px ${SANS}`;
    ctx.fillStyle = "#fff";
    ctx.textAlign = "left";
    ctx.fillText(state.score.toLocaleString("en-US"), 29, 70);
    label("BEST", 30, 90);
    ctx.font = `700 13px ${SANS}`;
    ctx.fillStyle = "#ffd23f";
    ctx.fillText(state.best_score.toLocaleString("en-US"), 72, 90);

    // Speed and driver, top right.
    const px = W - 16 - 156;
    panel(px, 16, 156, 132);
    drawGauge(px + 78, 84, 52);
    ctx.textBaseline = "alphabetic";

    const status = state.controller_status || "";
    const dh = status ? 64 : 46;
    panel(px, 156, 156, dh);
    label("DRIVER", px + 14, 176);
    const human = state.controller === "human";
    ctx.fillStyle = human ? "#3ddc84" : "#4fc3f7";
    ctx.beginPath();
    ctx.arc(px + 19, 191, 4, 0, Math.PI * 2);
    ctx.fill();
    ctx.font = `700 15px ${SANS}`;
    ctx.fillStyle = "#fff";
    ctx.textAlign = "left";
    ctx.fillText(fit(state.controller, 120), px + 30, 196);
    if (status) {
      ctx.font = `12px ${MONO}`;
      ctx.fillStyle = "rgba(255,255,255,0.75)";
      ctx.fillText(fit(status, 128), px + 14, 213);  // what the driver says about itself
    }

    // Run details, bottom left.
    ctx.font = `12px ${MONO}`;
    ctx.fillStyle = "rgba(255,255,255,0.8)";
    ctx.shadowColor = "rgba(0,0,0,0.6)";
    ctx.shadowBlur = 4;
    ctx.textAlign = "left";
    ctx.fillText(`seed ${state.seed}  run ${state.run_id}  pace ${state.pace || 1}`, 16, H - 16);
    ctx.shadowColor = "transparent";
    ctx.shadowBlur = 0;

    if (!state.game_over) {
      hints(human ? [["K", "left"], ["L", "right"]] : [["H", "take over"]], W / 2, H - 46);
    } else if (crashAge > 0.15) {
      drawCrash(W, H);
    }

    if (!connected) {
      ctx.font = `700 13px ${SANS}`;
      const text = "Connection lost, retrying…";
      const w = ctx.measureText(text).width + 28;
      ctx.fillStyle = "rgba(200,40,40,0.9)";
      ctx.beginPath();
      ctx.roundRect(W / 2 - w / 2, 16, w, 30, 15);
      ctx.fill();
      ctx.fillStyle = "#fff";
      ctx.textAlign = "center";
      ctx.textBaseline = "middle";
      ctx.fillText(text, W / 2, 31);
    }
  }

  function drawCrash(W, H) {
    ctx.fillStyle = "rgba(5,10,25,0.45)";
    ctx.fillRect(0, 0, W, H);

    const best = state.score > 0 && state.score >= state.best_score;
    const w = Math.min(340, W - 32), h = best ? 196 : 172;
    const x = W / 2 - w / 2, y = Math.max(16, H * 0.38 - h / 2);
    ctx.fillStyle = "rgba(10,20,42,0.82)";
    ctx.beginPath();
    ctx.roundRect(x, y, w, h, 16);
    ctx.fill();
    ctx.strokeStyle = "rgba(255,255,255,0.18)";
    ctx.lineWidth = 1;
    ctx.stroke();

    ctx.textAlign = "center";
    ctx.textBaseline = "alphabetic";
    ctx.font = `italic 900 52px ${SANS}`;
    ctx.fillStyle = "#ff4d4d";
    ctx.fillText("CRASH", W / 2, y + 66);
    label("SCORE", W / 2, y + 94, "center");
    ctx.font = `italic 800 28px ${SANS}`;
    ctx.fillStyle = "#fff";
    ctx.textAlign = "center";
    ctx.fillText(state.score.toLocaleString("en-US"), W / 2, y + 124);
    if (best) {
      ctx.font = `800 13px ${SANS}`;
      ctx.fillStyle = "#ffd23f";
      ctx.letterSpacing = "2px";
      ctx.fillText("NEW BEST", W / 2, y + 146);
      ctx.letterSpacing = "0px";
    }
    hints([["SPACE", "restart"]], W / 2, y + h - 40);
  }

  connect();
  requestAnimationFrame(draw);
})();
