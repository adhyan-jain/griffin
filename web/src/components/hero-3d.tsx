"use client";

/* eslint-disable react-hooks/immutability -- three.js uniforms, cameras and instanced matrices are mutable by design;
   the per-frame writes happen inside useFrame, outside React state, which this rule cannot model. */

import { Component, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Canvas, useFrame, useThree, type ThreeEvent } from "@react-three/fiber";
import { Html } from "@react-three/drei";
import { Bloom, EffectComposer } from "@react-three/postprocessing";
import { useTheme } from "next-themes";
import * as THREE from "three";
import { useFxMode, useReducedMotion } from "@/lib/fx";

export type OrbitNode = { id: string; status: string; title?: string };

const STATUS_COLOR: Record<string, string> = {
  RUNNING: "#5aa2ff",
  VERIFYING: "#5aa2ff",
  RETRYING: "#e8a838",
  QUEUED: "#e8a838",
  CREATED: "#e8a838",
  ROUTED: "#e8a838",
  WAITING_FOR_DEPENDENCY: "#8a93a6",
  AWAITING_APPROVAL: "#ffd166",
  COMPLETED: "#4cc38a",
  FAILED: "#ef5b5b",
  CANCELLED: "#6b7280",
};
const LIVE = new Set(["RUNNING", "VERIFYING", "RETRYING"]);
const MAX_NODES = 28;
/** Finished work recedes to faint dust; what needs attention glows. */
const DONE = new Set(["COMPLETED", "CANCELLED"]);
const weight = (status: string) => (LIVE.has(status) ? 1 : DONE.has(status) ? 0.38 : 0.8);

// ---- JJ core: displaced brass sphere with a fresnel rim, plus a soft aura -------------------------

const CORE_VERT = /* glsl */ `
  uniform float uTime;
  uniform float uActivity;
  varying vec3 vN;
  varying vec3 vV;
  varying float vD;
  float h(vec3 p) { p = fract(p * 0.3183099 + 0.1); p *= 17.0; return fract(p.x * p.y * p.z * (p.x + p.y + p.z)); }
  float n3(vec3 x) {
    vec3 i = floor(x), f = fract(x);
    f = f * f * (3.0 - 2.0 * f);
    return mix(mix(mix(h(i), h(i + vec3(1,0,0)), f.x), mix(h(i + vec3(0,1,0)), h(i + vec3(1,1,0)), f.x), f.y),
               mix(mix(h(i + vec3(0,0,1)), h(i + vec3(1,0,1)), f.x), mix(h(i + vec3(0,1,1)), h(i + vec3(1,1,1)), f.x), f.y), f.z);
  }
  void main() {
    float amp = 0.05 + 0.08 * uActivity;
    float d = n3(position * 1.7 + uTime * (0.25 + 0.5 * uActivity)) - 0.5;
    vec3 p = position + normal * d * amp * 2.0;
    vD = d;
    vec4 mv = modelViewMatrix * vec4(p, 1.0);
    vN = normalize(normalMatrix * normal);
    vV = normalize(-mv.xyz);
    gl_Position = projectionMatrix * mv;
  }
`;

const CORE_FRAG = /* glsl */ `
  precision highp float;
  uniform vec3 uBrass;
  uniform float uActivity;
  varying vec3 vN;
  varying vec3 vV;
  varying float vD;
  void main() {
    vec3 n = normalize(vN);
    vec3 v = normalize(vV);
    float fres = pow(1.0 - max(dot(n, v), 0.0), 4.0);
    // Obsidian body: near-black ink with a faint blue sheen where the displacement rises.
    vec3 body = mix(vec3(0.018, 0.022, 0.038), vec3(0.055, 0.070, 0.125), smoothstep(-0.30, 0.30, vD));
    // Molten veins: thin brass lines where the moving displacement crosses fixed levels.
    float veins = smoothstep(0.030, 0.0, abs(vD - 0.07))
                + 0.7 * smoothstep(0.022, 0.0, abs(vD + 0.10))
                + 0.5 * smoothstep(0.018, 0.0, abs(vD - 0.20));
    vec3 col = body + uBrass * veins * (0.9 + 1.3 * uActivity);
    // Rim light: brass at the edge, a pinpoint of warm white at the very silhouette.
    col += uBrass * fres * (0.22 + 0.35 * uActivity);
    col += vec3(1.0, 0.93, 0.75) * pow(fres, 3.0) * 0.28;
    gl_FragColor = vec4(col, 1.0);
  }
`;

const AURA_VERT = /* glsl */ `
  varying vec3 vN;
  void main() {
    vN = normalize(normalMatrix * normal);
    gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
  }
`;
const AURA_FRAG = /* glsl */ `
  precision highp float;
  uniform vec3 uColor;
  uniform float uPower;
  varying vec3 vN;
  void main() {
    float i = pow(max(0.0, 0.68 - dot(vN, vec3(0.0, 0.0, 1.0))), 2.2);
    gl_FragColor = vec4(uColor, clamp(i * uPower, 0.0, 1.0));
  }
`;

type Drag = { down: boolean; x: number; y: number; vx: number; vy: number };

function Scene({
  nodes,
  animated,
  lite,
  dark,
  activity,
  drag,
  onSelect,
}: {
  nodes: OrbitNode[];
  animated: boolean;
  lite: boolean;
  dark: boolean;
  activity: number;
  drag: React.RefObject<Drag>;
  onSelect?: (id: string) => void;
}) {
  const { invalidate } = useThree();
  const tilt = useRef<THREE.Group>(null);
  const spin = useRef<THREE.Group>(null);
  const inst = useRef<THREE.InstancedMesh>(null);
  const chip = useRef<THREE.Group>(null);
  const [hover, setHover] = useState<number | null>(null);
  const smooth = useRef({ act: 0.1, rot: 0 });
  // R3F copies the `uniforms` prop, so live values are written through the material refs.
  const coreMat = useRef<THREE.ShaderMaterial>(null);
  const auraMat = useRef<THREE.ShaderMaterial>(null);
  const dummy = useMemo(() => new THREE.Object3D(), []);
  const color = useMemo(() => new THREE.Color(), []);

  const brass = dark ? "#e3b648" : "#a97b12";
  const shown = useMemo(() => nodes.slice(0, MAX_NODES), [nodes]);

  // One orbit per node, spread over three tilted rings; live tasks move faster.
  const orbits = useMemo(() => {
    const golden = Math.PI * (3 - Math.sqrt(5));
    return shown.map((n, i) => ({
      ring: i % 3,
      r: 1.9 + (i % 3) * 0.42,
      phase: i * golden,
      speed: (LIVE.has(n.status) ? 0.55 : 0.18) * (i % 2 ? 1 : -1) * (0.8 + ((i * 37) % 10) / 25),
    }));
  }, [shown]);
  const ringTilt = useMemo(() => [new THREE.Euler(1.05, 0.2, 0), new THREE.Euler(0.55, -0.7, 0.4), new THREE.Euler(1.35, 0.9, -0.3)], []);

  const coreUniforms = useMemo(
    () => ({ uTime: { value: 0 }, uActivity: { value: 0.1 }, uBrass: { value: new THREE.Color("#e3b648") } }),
    [],
  );
  const auraUniforms = useMemo(() => ({ uColor: { value: new THREE.Color("#e3b648") }, uPower: { value: 0.22 } }), []);
  useEffect(() => {
    const c = coreMat.current?.uniforms;
    const a = auraMat.current?.uniforms;
    if (!c || !a) return;
    c.uBrass.value.set(brass);
    a.uColor.value.set(brass);
    invalidate();
  }, [brass, dark, invalidate]);

  // Paint colours when the set of tasks or their statuses change.
  useEffect(() => {
    const m = inst.current;
    if (!m) return;
    shown.forEach((n, i) => m.setColorAt(i, color.set(STATUS_COLOR[n.status] ?? "#8a93a6").multiplyScalar(DONE.has(n.status) ? 0.5 : 1)));
    if (m.instanceColor) m.instanceColor.needsUpdate = true;
    invalidate();
  }, [shown, color, invalidate]);

  useFrame((state, dt) => {
    const t = animated ? state.clock.elapsedTime : 0;
    const k = smooth.current;
    k.act += (activity - k.act) * Math.min(1, dt * 2);
    const cu = coreMat.current?.uniforms;
    if (cu) {
      cu.uTime.value = t;
      cu.uActivity.value = k.act;
    }

    const d = drag.current;
    if (spin.current) {
      if (animated && !d.down) d.vy *= Math.exp(-dt * 2.2); // inertia after a fling
      k.rot += (animated ? dt * 0.16 : 0) + d.vy * dt;
      spin.current.rotation.y = k.rot;
    }
    if (tilt.current) {
      tilt.current.rotation.x = THREE.MathUtils.damp(tilt.current.rotation.x, animated ? -state.pointer.y * 0.3 : 0, 3, dt);
      tilt.current.rotation.y = THREE.MathUtils.damp(tilt.current.rotation.y, animated ? state.pointer.x * 0.4 : 0, 3, dt);
    }

    // Entrance dolly: the camera glides in once, then holds.
    if (animated && state.camera.position.z > 7.15) {
      state.camera.position.z = THREE.MathUtils.damp(state.camera.position.z, 7.1, 2.6, dt);
    } else if (!animated) {
      state.camera.position.z = 7.1;
    }

    const m = inst.current;
    if (m) {
      shown.forEach((n, i) => {
        const o = orbits[i];
        const a = o.phase + t * o.speed;
        dummy.position.set(Math.cos(a) * o.r, 0, Math.sin(a) * o.r).applyEuler(ringTilt[o.ring]);
        const live = LIVE.has(n.status);
        const pulse = live && animated ? 1 + Math.sin(t * 4 + i) * 0.3 : 1;
        dummy.scale.setScalar((hover === i ? 1.9 : 1) * pulse * weight(n.status));
        dummy.updateMatrix();
        m.setMatrixAt(i, dummy.matrix);
        if (hover === i && chip.current) chip.current.position.copy(dummy.position);
      });
      m.instanceMatrix.needsUpdate = true;
    }
  });

  const over = (e: ThreeEvent<PointerEvent>) => {
    e.stopPropagation();
    if (e.instanceId !== undefined && e.instanceId !== hover) setHover(e.instanceId);
    document.body.style.cursor = "pointer";
  };
  const out = () => {
    setHover(null);
    document.body.style.cursor = "";
  };
  useEffect(
    () => () => {
      document.body.style.cursor = "";
    },
    [],
  );

  const hovered = hover !== null ? shown[hover] : null;

  return (
    <group ref={tilt}>
      <group ref={spin}>
        <mesh>
          <icosahedronGeometry args={[1.0, lite ? 6 : 12]} />
          <shaderMaterial ref={coreMat} vertexShader={CORE_VERT} fragmentShader={CORE_FRAG} uniforms={coreUniforms} />
        </mesh>
        {/* The shell fakes a glow where there is no bloom pass (Lite, light theme). With bloom it would double up into a halo. */}
        {(lite || !dark) && (
          <mesh scale={1.3}>
            <sphereGeometry args={[1, lite ? 24 : 40, lite ? 24 : 40]} />
            <shaderMaterial
              ref={auraMat}
              vertexShader={AURA_VERT}
              fragmentShader={AURA_FRAG}
              uniforms={auraUniforms}
              side={THREE.BackSide}
              transparent
              depthWrite={false}
              blending={dark ? THREE.AdditiveBlending : THREE.NormalBlending}
            />
          </mesh>
        )}
        {ringTilt.map((rot, i) => (
          <mesh key={i} rotation={rot}>
            <torusGeometry args={[1.9 + i * 0.42, 0.004, 6, lite ? 96 : 192]} />
            <meshBasicMaterial color={brass} transparent opacity={0.34 - i * 0.07} />
          </mesh>
        ))}
        {shown.length > 0 && (
          <instancedMesh
            key={shown.length}
            ref={inst}
            args={[undefined, undefined, shown.length]}
            onPointerMove={over}
            onPointerOut={out}
            onClick={(e) => {
              e.stopPropagation();
              // A drag that ends over a satellite is a rotation, not a tap.
              if (e.delta > 5 || e.instanceId === undefined) return;
              const n = shown[e.instanceId];
              if (n) onSelect?.(n.id);
            }}
          >
            <sphereGeometry args={[0.1, 14, 14]} />
            <meshBasicMaterial toneMapped={false} />
          </instancedMesh>
        )}
        <group ref={chip}>
          {hovered && (
            <Html center distanceFactor={7} zIndexRange={[20, 0]} style={{ pointerEvents: "none" }}>
              <div className="-translate-y-7 whitespace-nowrap rounded-full border border-white/10 bg-black/70 px-2.5 py-1 text-[11px] font-medium text-white shadow-lg backdrop-blur">
                <span className="mr-1.5 inline-block size-1.5 rounded-full align-middle" style={{ background: STATUS_COLOR[hovered.status] ?? "#8a93a6" }} />
                {(hovered.title || hovered.status).slice(0, 34)}
              </div>
            </Html>
          )}
        </group>
      </group>
    </group>
  );
}

class Boundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  render() {
    return this.state.failed ? null : this.props.children;
  }
}

/** Keeps the demand-driven canvas ticking (capped fps, paused while hidden). */
function Ticker({ fps, animated }: { fps: number; animated: boolean }) {
  const invalidate = useThree((s) => s.invalidate);
  useEffect(() => {
    invalidate();
    if (!animated) return;
    let raf = 0;
    let last = 0;
    const interval = 1000 / fps - 2;
    const tick = (now: number) => {
      raf = requestAnimationFrame(tick);
      if (document.hidden || now - last < interval) return;
      last = now;
      invalidate();
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, [fps, animated, invalidate]);
  return null;
}

/**
 * The JJ core: a living brass sphere with one satellite per task, colored by status. Drag to spin it
 * (it keeps its momentum), hover a satellite for the task title, tap one to open it. Decorative to
 * assistive tech (the task list below carries the same information).
 */
export default function Hero3D({
  nodes,
  onSelect,
  className,
}: {
  nodes: OrbitNode[];
  onSelect?: (id: string) => void;
  className?: string;
}) {
  const { resolvedTheme } = useTheme();
  const [mode] = useFxMode();
  const reduced = useReducedMotion();
  const drag = useRef<Drag>({ down: false, x: 0, y: 0, vx: 0, vy: 0 });
  const live = nodes.filter((n) => LIVE.has(n.status)).length;
  const activity = Math.min(1, 0.1 + live / 6);
  const dark = resolvedTheme !== "light";

  if (mode === "off") return null;
  const lite = mode === "lite";
  const animated = !reduced;

  const down = (e: React.PointerEvent) => {
    drag.current = { down: true, x: e.clientX, y: e.clientY, vx: 0, vy: 0 };
  };
  const move = (e: React.PointerEvent) => {
    const d = drag.current;
    if (!d.down) return;
    d.vy = (e.clientX - d.x) * 0.06;
    d.x = e.clientX;
    d.y = e.clientY;
  };
  const up = () => {
    drag.current.down = false;
  };

  return (
    <div
      className={className}
      style={{ touchAction: "pan-y" }}
      onPointerDown={down}
      onPointerMove={move}
      onPointerUp={up}
      onPointerCancel={up}
      onPointerLeave={up}
    >
      <Boundary>
        <Canvas
          dpr={lite ? 1 : [1, 1.75]}
          camera={{ position: [0, 0, animated ? 11 : 7.1], fov: 38 }}
          frameloop="demand"
          gl={{ antialias: true, alpha: true, powerPreference: lite ? "low-power" : "default" }}
        >
          <Ticker fps={lite ? 30 : 60} animated={animated} />
          <Scene nodes={nodes} animated={animated} lite={lite} dark={dark} activity={activity} drag={drag} onSelect={onSelect} />
          {/* Bloom makes the molten core and the live nodes glow. Skipped in Lite (a full-screen pass) and in light theme. */}
          {!lite && dark && (
            <EffectComposer multisampling={0}>
              <Bloom mipmapBlur intensity={0.35 + activity * 0.4} luminanceThreshold={0.86} luminanceSmoothing={0.2} radius={0.6} />
            </EffectComposer>
          )}
        </Canvas>
      </Boundary>
    </div>
  );
}
