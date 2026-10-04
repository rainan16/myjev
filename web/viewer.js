// Viewer: draws the state stream from /ws and sends K / L as input.
// It holds no game logic; everything it shows comes from the API.
// Curves, scenery and smoothing are cosmetic only.
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
  const FONT = "ui-monospace, Menlo, monospace";

  const CAR_COLOURS = ["#2f6fdb", "#e8e8e8", "#f2b632", "#2aa876", "#8e5bd6", "#e2703a"];

  let state = null;
  let stateAt = 0;      // when the latest state arrived
  let connected = false;
  let ws = null;
  let shownLane = null; // eased towards state.lane
  let scenery = 0;      // horizontal scroll of the backdrop
  let lastNose = null;

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

  // ---------- backdrop ----------

  function ridge(x, seed) {
    return 0.5 + 0.28 * Math.sin(x * 0.011 + seed) + 0.16 * Math.sin(x * 0.027 + seed * 2.1)
      + 0.06 * Math.sin(x * 0.071 + seed * 3.7);
  }

  function drawMountains(W, horizon, shift, height, seed, colour) {
    ctx.fillStyle = colour;
    ctx.beginPath();
    ctx.moveTo(0, horizon);
    for (let x = 0; x <= W + 8; x += 8) {
      ctx.lineTo(x, horizon - height * ridge(x + shift, seed));
    }
    ctx.lineTo(W, horizon);
    ctx.fill();
  }

  function drawCloud(x, y, r) {
    ctx.beginPath();
    ctx.ellipse(x, y, r * 1.6, r * 0.55, 0, 0, Math.PI * 2);
    ctx.ellipse(x - r * 0.7, y - r * 0.3, r * 0.8, r * 0.5, 0, 0, Math.PI * 2);
    ctx.ellipse(x + r * 0.5, y - r * 0.4, r * 0.9, r * 0.6, 0, 0, Math.PI * 2);
    ctx.fill();
  }

  function drawBackdrop(W, H, horizon) {
    const sky = ctx.createLinearGradient(0, 0, 0, horizon);
    sky.addColorStop(0, "#1e5fbf");
    sky.addColorStop(0.65, "#6fb4ee");
    sky.addColorStop(1, "#dff0fb");
    ctx.fillStyle = sky;
    ctx.fillRect(0, 0, W, horizon + 1);

    const sunX = W * 0.72 - scenery * 0.05, sunY = horizon * 0.42;
    const glow = ctx.createRadialGradient(sunX, sunY, 0, sunX, sunY, horizon * 0.5);
    glow.addColorStop(0, "rgba(255,250,220,0.95)");
    glow.addColorStop(0.12, "rgba(255,244,200,0.6)");
    glow.addColorStop(1, "rgba(255,244,200,0)");
    ctx.fillStyle = glow;
    ctx.fillRect(0, 0, W, horizon);

    ctx.fillStyle = "rgba(255,255,255,0.85)";
    const span = W + 400;
    for (let i = 0; i < 6; i++) {
      const x = ((i * 397 - scenery * 0.15) % span + span) % span - 200;
      drawCloud(x, horizon * (0.18 + 0.11 * ((i * 7) % 5)), horizon * (0.05 + 0.012 * (i % 3)));
    }

    drawMountains(W, horizon, scenery * 0.3, horizon * 0.34, 1.7, "#8fa9c9");
    drawMountains(W, horizon, scenery * 0.6 + 500, horizon * 0.18, 4.2, "#5d8a6a");
  }

  // ---------- sprites, drawn in lane units with the origin on the ground ----------

  function shadow(w) {
    ctx.fillStyle = "rgba(0,0,0,0.3)";
    ctx.beginPath();
    ctx.ellipse(0, 0, w, w * 0.14, 0, 0, Math.PI * 2);
    ctx.fill();
  }

  function drawCar(colour, braking) {
    const w = 0.62, hw = w / 2;
    shadow(hw * 1.15);

    ctx.fillStyle = "#111";                                   // tyres
    ctx.fillRect(-hw, -0.12, 0.13, 0.12);
    ctx.fillRect(hw - 0.13, -0.12, 0.13, 0.12);

    ctx.fillStyle = colour;                                   // body
    ctx.beginPath();
    ctx.roundRect(-hw, -0.3, w, 0.24, 0.04);
    ctx.fill();
    ctx.beginPath();                                          // cabin
    ctx.moveTo(-hw + 0.05, -0.29);
    ctx.lineTo(-hw + 0.12, -0.45);
    ctx.lineTo(hw - 0.12, -0.45);
    ctx.lineTo(hw - 0.05, -0.29);
    ctx.fill();

    ctx.fillStyle = "rgba(0,0,0,0.22)";                       // shading under the boot lid
    ctx.fillRect(-hw, -0.15, w, 0.09);

    ctx.fillStyle = "#1b2733";                                // rear window
    ctx.beginPath();
    ctx.moveTo(-hw + 0.1, -0.3);
    ctx.lineTo(-hw + 0.15, -0.42);
    ctx.lineTo(hw - 0.15, -0.42);
    ctx.lineTo(hw - 0.1, -0.3);
    ctx.fill();
    ctx.fillStyle = "rgba(255,255,255,0.25)";                 // glass reflection
    ctx.beginPath();
    ctx.moveTo(-hw + 0.13, -0.31);
    ctx.lineTo(-hw + 0.17, -0.41);
    ctx.lineTo(-0.04, -0.41);
    ctx.lineTo(-0.1, -0.31);
    ctx.fill();

    ctx.fillStyle = braking ? "#ff3b30" : "#b3161a";          // tail lights
    ctx.fillRect(-hw + 0.03, -0.26, 0.13, 0.05);
    ctx.fillRect(hw - 0.16, -0.26, 0.13, 0.05);
    ctx.fillStyle = "#e9e9e9";                                // plate
    ctx.fillRect(-0.07, -0.2, 0.14, 0.05);
    ctx.fillStyle = "#2a2a2a";                                // bumper
    ctx.fillRect(-hw, -0.1, w, 0.04);
  }

  function drawBarrier() {
    const w = 0.7, hw = w / 2;
    shadow(hw * 1.1);
    ctx.fillStyle = "#4a4a4a";                                // legs
    ctx.fillRect(-hw + 0.05, -0.3, 0.05, 0.3);
    ctx.fillRect(hw - 0.1, -0.3, 0.05, 0.3);

    for (const top of [-0.44, -0.22]) {                       // two striped boards
      ctx.save();
      ctx.beginPath();
      ctx.rect(-hw, top, w, 0.15);
      ctx.clip();
      ctx.fillStyle = "#f4f4f4";
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
      ctx.restore();
    }
    ctx.fillStyle = "#ffb300";                                // warning lamps
    ctx.beginPath();
    ctx.arc(-hw + 0.075, -0.48, 0.04, 0, Math.PI * 2);
    ctx.arc(hw - 0.075, -0.48, 0.04, 0, Math.PI * 2);
    ctx.fill();
  }

  function drawPalm(lean) {
    shadow(0.35);
    ctx.strokeStyle = "#7a5230";
    ctx.lineWidth = 0.14;
    ctx.lineCap = "round";
    ctx.beginPath();
    ctx.moveTo(0, 0);
    ctx.quadraticCurveTo(lean * 0.1, -1.2, lean * 0.35, -2.2);
    ctx.stroke();

    ctx.translate(lean * 0.35, -2.2);
    ctx.lineWidth = 0.13;
    for (let i = 0; i < 7; i++) {
      const dir = -1 + i / 3;                                  // -1 .. 1 across the crown
      ctx.strokeStyle = i % 2 ? "#2f8f3e" : "#236e30";
      ctx.beginPath();
      ctx.moveTo(0, 0);
      ctx.quadraticCurveTo(dir * 0.6, -0.55 + Math.abs(dir) * 0.3, dir * 1.05, 0.25 + Math.abs(dir) * 0.1);
      ctx.stroke();
    }
  }

  function drawBush() {
    shadow(0.4);
    ctx.fillStyle = "#2c7a3a";
    ctx.beginPath();
    ctx.arc(-0.2, -0.2, 0.25, 0, Math.PI * 2);
    ctx.arc(0.18, -0.18, 0.22, 0, Math.PI * 2);
    ctx.arc(0, -0.36, 0.26, 0, Math.PI * 2);
    ctx.fill();
    ctx.fillStyle = "#3d9a4c";
    ctx.beginPath();
    ctx.arc(-0.05, -0.4, 0.14, 0, Math.PI * 2);
    ctx.fill();
  }

  // ---------- frame ----------

  function draw(now) {
    requestAnimationFrame(draw);
    const dpr = window.devicePixelRatio || 1;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    const W = innerWidth, H = innerHeight;

    if (!state) {
      ctx.fillStyle = "#000";
      ctx.fillRect(0, 0, W, H);
      ctx.fillStyle = "#fff";
      ctx.font = `16px ${FONT}`;
      ctx.textAlign = "center";
      ctx.fillText("connecting...", W / 2, H / 2);
      return;
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

    drawBackdrop(W, H, horizon);

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

    ctx.fillStyle = "#4f9a45";
    ctx.fillRect(0, horizon, W, roadH);

    const half = lanes / 2;
    for (let i = pts.length - 2; i >= 0; i--) {
      const p = pts[i], q = pts[i + 1];
      const even = Math.floor((nose + (p.a + q.a) / 2) / SEG) % 2 === 0;

      ctx.fillStyle = even ? "#4f9a45" : "#458c3c";
      ctx.fillRect(0, q.y, W, p.y - q.y + 1);

      quad(p, q, -half - 0.28, half + 0.28, even ? "#d9d9d9" : "#c0392b");   // rumble strips
      quad(p, q, -half, half, even ? "#5c5f66" : "#565960");                 // tarmac
      quad(p, q, -half + 0.03, -half + 0.1, "#eeeeee");                      // edge lines
      quad(p, q, half - 0.1, half - 0.03, "#eeeeee");
      if (even) {
        for (let b = 1; b < lanes; b++) quad(p, q, b - half - 0.035, b - half + 0.035, "#eeeeee");
      }
    }

    // Distance haze.
    const haze = ctx.createLinearGradient(0, horizon, 0, horizon + roadH * 0.22);
    haze.addColorStop(0, "rgba(223,240,251,0.75)");
    haze.addColorStop(1, "rgba(223,240,251,0)");
    ctx.fillStyle = haze;
    ctx.fillRect(0, horizon, W, roadH * 0.22);

    // Everything standing on the ground, far to near.
    const sprites = [];
    for (let z = Math.ceil((nose + NEAREST + 2) / TREE_EVERY) * TREE_EVERY; z - nose < VIEW; z += TREE_EVERY) {
      const n = Math.round(z / TREE_EVERY);
      const palm = n % 3 !== 0;
      for (const side of [-1, 1]) {
        sprites.push({
          a: z - nose,
          x: side * (half + (palm ? 1.0 : 1.5)),
          paint: palm ? () => drawPalm(-side) : drawBush,
        });
      }
    }
    for (const o of state.obstacles) {
      // Keep it on screen for exactly as long as the core can still collide with it.
      if (o.ahead > VIEW || o.ahead + o.length <= -CAR_LENGTH) continue;
      const a = Math.max(o.ahead - since * (state.speed - o.speed), NEAREST + 0.2);
      sprites.push({
        a,
        x: o.lane - (lanes - 1) / 2,
        paint: o.type === "car" ? () => drawCar(CAR_COLOURS[o.id % CAR_COLOURS.length], false) : drawBarrier,
      });
    }

    if (shownLane === null) shownLane = state.lane;
    const slide = state.lane - shownLane;
    shownLane += slide * 0.3;
    sprites.push({
      a: -CAR_LENGTH,
      x: shownLane - (lanes - 1) / 2,
      tilt: state.game_over ? 0.12 : slide * 0.1,
      paint: () => drawCar("#d81e2c", state.game_over),
    });

    sprites.sort((s1, s2) => s2.a - s1.a);
    for (const sp of sprites) {
      const s = NEAR / (sp.a + CAR_LENGTH + CAM_BACK);
      const unit = lanePx * s;
      ctx.save();
      ctx.globalAlpha = clamp01((VIEW - sp.a) / 40);
      ctx.translate(W / 2 + (sp.x + offsetAt(sp.a)) * unit, horizon + roadH * s);
      ctx.scale(unit, unit);
      if (sp.tilt) ctx.rotate(sp.tilt);
      sp.paint();
      ctx.restore();
    }

    drawHud(W, H, horizon);
  }

  function drawHud(W, H, horizon) {
    ctx.font = `bold 16px ${FONT}`;
    ctx.textBaseline = "top";
    ctx.shadowColor = "rgba(0,0,0,0.6)";
    ctx.shadowBlur = 4;
    ctx.shadowOffsetY = 1;

    ctx.fillStyle = "#fff";
    ctx.textAlign = "left";
    ctx.fillText(`SCORE ${state.score}`, 16, 14);
    ctx.fillText(`BEST  ${state.best_score}`, 16, 36);
    ctx.textAlign = "right";
    ctx.fillText(`SPEED ${Math.round(state.speed)}`, W - 16, 14);
    ctx.fillText(`DRIVER ${state.controller}`, W - 16, 36);
    ctx.font = `13px ${FONT}`;
    ctx.fillStyle = "rgba(255,255,255,0.8)";
    // What the driver says about itself goes under its name.
    const status = state.controller_status || "";
    if (status) ctx.fillText(status, W - 16, 60);
    ctx.fillText(`seed ${state.seed}  run ${state.run_id}  pace ${state.pace || 1}`, W - 16, status ? 80 : 60);
    ctx.textAlign = "left";
    ctx.fillText(state.controller === "human" ? "K left   L right" : "H take over", 16, 60);

    ctx.textAlign = "center";
    if (state.game_over) {
      ctx.shadowColor = "transparent";
      ctx.fillStyle = "rgba(0,0,0,0.45)";
      ctx.fillRect(0, 0, W, H);
      ctx.fillStyle = "#ff4d4d";
      ctx.font = `bold 56px ${FONT}`;
      ctx.fillText("CRASH", W / 2, horizon * 0.4);
      ctx.fillStyle = "#fff";
      ctx.font = `18px ${FONT}`;
      ctx.fillText(`score ${state.score}   press SPACE to restart`, W / 2, horizon * 0.4 + 70);
    }
    if (!connected) {
      ctx.fillStyle = "#ff4d4d";
      ctx.font = `bold 16px ${FONT}`;
      ctx.fillText("connection lost, retrying...", W / 2, H - 30);
    }
    ctx.shadowColor = "transparent";
    ctx.shadowBlur = 0;
    ctx.shadowOffsetY = 0;
  }

  connect();
  requestAnimationFrame(draw);
})();
