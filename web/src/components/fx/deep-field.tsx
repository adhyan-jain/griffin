"use client";

/* eslint-disable react-hooks/immutability -- three.js uniforms, cameras and instanced matrices are mutable by design;
   the per-frame writes happen inside useFrame, outside React state, which this rule cannot model. */

import { Component, useEffect, useMemo, useRef, type ReactNode } from "react";
import { Canvas, useFrame, useThree } from "@react-three/fiber";
import { useTheme } from "next-themes";
import * as THREE from "three";
import { readActivity, useFxMode, useReducedMotion, type FxMode } from "@/lib/fx";

/**
 * The Aerie: one fixed WebGL layer behind every screen. A slow domain-warped nebula in ink and
 * brass (it brightens and pulses as more work runs) with depth-layered dust that parallaxes with
 * the pointer and the scroll. Purely decorative: aria-hidden, no pointer events, capped frame rate,
 * paused while the tab is hidden, a single static frame under reduced motion.
 */

const NEBULA_VERT = /* glsl */ `
  varying vec2 vUv;
  void main() {
    vUv = position.xy * 0.5 + 0.5;
    gl_Position = vec4(position.xy, 0.0, 1.0);
  }
`;

const NEBULA_FRAG = /* glsl */ `
  precision highp float;
  varying vec2 vUv;
  uniform float uTime;
  uniform float uActivity;
  uniform float uDark;
  uniform float uAspect;
  uniform vec2 uPointer;

  float hash(vec2 p) { p = fract(p * vec2(123.34, 456.21)); p += dot(p, p + 45.32); return fract(p.x * p.y); }
  float noise(vec2 p) {
    vec2 i = floor(p), f = fract(p);
    f = f * f * (3.0 - 2.0 * f);
    return mix(mix(hash(i), hash(i + vec2(1.0, 0.0)), f.x), mix(hash(i + vec2(0.0, 1.0)), hash(i + vec2(1.0, 1.0)), f.x), f.y);
  }
  float fbm(vec2 p) {
    float v = 0.0, a = 0.5;
    for (int i = 0; i < 4; i++) { v += a * noise(p); p = p * 2.02 + vec2(1.7, 9.2); a *= 0.5; }
    return v;
  }

  void main() {
    vec2 uv = (vUv - 0.5) * vec2(uAspect, 1.0);
    float t = uTime * 0.035;
    vec2 q = vec2(fbm(uv * 1.5 + t), fbm(uv * 1.5 + vec2(5.2, 1.3) - t));
    float f = fbm(uv * 1.3 + 2.1 * q + uPointer * 0.10 + t * 0.6);
    float ridge = smoothstep(0.46, 0.92, f);

    // Dark: ink base (matches --background) with blue depth and brass veins.
    vec3 ink = vec3(0.078, 0.086, 0.110);
    vec3 blue = vec3(0.100, 0.230, 0.560);
    vec3 violet = vec3(0.330, 0.150, 0.560);
    vec3 brass = vec3(0.890, 0.710, 0.280);
    vec3 dark = mix(ink, mix(blue, violet, q.y), smoothstep(0.22, 0.80, f) * 0.80);
    dark += brass * ridge * (0.12 + 0.30 * uActivity) * (0.5 + 0.5 * q.x);

    // Slow pulse rings radiating from low centre; stronger with activity.
    float d = length(uv - vec2(0.0, -0.25));
    float ring = exp(-pow((fract(d * 0.85 - uTime * 0.07) - 0.5) * 7.0, 2.0));
    dark += brass * ring * uActivity * 0.12 * exp(-d * 1.3);

    // Light: warm paper with the faintest brass haze so cards still read as glass.
    vec3 paper = vec3(0.985, 0.980, 0.962);
    vec3 light = mix(paper, vec3(0.95, 0.88, 0.70), ridge * 0.16 + f * 0.05);

    vec3 col = mix(light, dark, uDark);
    float vig = smoothstep(1.35, 0.15, length(uv));
    col = mix(col * mix(0.97, 0.6, uDark), col, vig);
    gl_FragColor = vec4(col, 1.0);
  }
`;

const DUST_VERT = /* glsl */ `
  attribute float aSeed;
  uniform float uTime;
  uniform float uPx;
  uniform float uActivity;
  varying float vA;
  void main() {
    vec3 p = position;
    p.y = mod(p.y + uTime * (0.05 + 0.13 * aSeed) + 6.0, 12.0) - 6.0;
    p.x += sin(uTime * 0.2 + aSeed * 20.0) * 0.16;
    vec4 mv = modelViewMatrix * vec4(p, 1.0);
    gl_Position = projectionMatrix * mv;
    gl_PointSize = (1.4 + aSeed * 3.4) * uPx * (1.0 + uActivity * 0.6) * (6.0 / -mv.z);
    vA = 0.25 + 0.75 * fract(aSeed * 7.0 + uTime * 0.28 * (0.3 + aSeed));
  }
`;

const DUST_FRAG = /* glsl */ `
  precision highp float;
  uniform vec3 uColor;
  uniform float uOpacity;
  varying float vA;
  void main() {
    float d = length(gl_PointCoord - 0.5);
    float a = smoothstep(0.5, 0.0, d);
    gl_FragColor = vec4(uColor, a * a * vA * uOpacity);
  }
`;

function Field({ mode, animated, dark }: { mode: Exclude<FxMode, "off">; animated: boolean; dark: boolean }) {
  const lite = mode === "lite";
  const { size, gl, invalidate } = useThree();
  const smooth = useRef({ x: 0, y: 0, act: 0.12 });
  // R3F copies the `uniforms` prop, so live values are written through the material refs.
  const nebulaMat = useRef<THREE.ShaderMaterial>(null);
  const dustMat = useRef<THREE.ShaderMaterial>(null);

  const dustGeo = useMemo(() => {
    const n = lite ? 320 : 900;
    const pos = new Float32Array(n * 3);
    const seed = new Float32Array(n);
    // Deterministic so a re-render never reshuffles the sky.
    let s = 1337;
    const rnd = () => ((s = (s * 16807) % 2147483647) - 1) / 2147483646;
    for (let i = 0; i < n; i++) {
      pos[i * 3] = (rnd() - 0.5) * 18;
      pos[i * 3 + 1] = (rnd() - 0.5) * 12;
      pos[i * 3 + 2] = -8 + rnd() * 10;
      seed[i] = rnd();
    }
    const g = new THREE.BufferGeometry();
    g.setAttribute("position", new THREE.BufferAttribute(pos, 3));
    g.setAttribute("aSeed", new THREE.BufferAttribute(seed, 1));
    return g;
  }, [lite]);
  useEffect(() => () => dustGeo.dispose(), [dustGeo]);

  const nebulaUniforms = useMemo(
    () => ({ uTime: { value: 0 }, uActivity: { value: 0.12 }, uDark: { value: 1 }, uAspect: { value: 1 }, uPointer: { value: new THREE.Vector2() } }),
    [],
  );
  const dustUniforms = useMemo(
    () => ({ uTime: { value: 0 }, uPx: { value: 1 }, uActivity: { value: 0.12 }, uColor: { value: new THREE.Color("#e3b648") }, uOpacity: { value: 0.8 } }),
    [],
  );

  // Theme and size changes must repaint even when nothing is animating (reduced motion).
  useEffect(() => {
    const n = nebulaMat.current?.uniforms;
    const d = dustMat.current?.uniforms;
    if (!n || !d) return;
    n.uDark.value = dark ? 1 : 0;
    d.uColor.value.set(dark ? "#e8c46a" : "#a97b12");
    d.uOpacity.value = dark ? 0.85 : 0.35;
    invalidate();
  }, [dark, invalidate]);
  useEffect(() => {
    const n = nebulaMat.current?.uniforms;
    const d = dustMat.current?.uniforms;
    if (!n || !d) return;
    n.uAspect.value = size.width / Math.max(size.height, 1);
    d.uPx.value = gl.getPixelRatio() * Math.min(1.4, Math.max(0.8, size.height / 800));
    invalidate();
  }, [size, gl, invalidate]);

  useFrame((state, dt) => {
    const t = animated ? state.clock.elapsedTime : 0;
    const k = smooth.current;
    k.act += (readActivity() - k.act) * Math.min(1, dt * 1.5);
    k.x = THREE.MathUtils.damp(k.x, state.pointer.x, 2.2, dt);
    k.y = THREE.MathUtils.damp(k.y, state.pointer.y, 2.2, dt);
    const n = nebulaMat.current?.uniforms;
    const d = dustMat.current?.uniforms;
    if (n && d) {
      n.uTime.value = t;
      n.uActivity.value = k.act;
      n.uPointer.value.set(k.x, k.y);
      d.uTime.value = t;
      d.uActivity.value = k.act;
    }
    // Camera drifts with the pointer and slides with the page scroll: the depth cue.
    const scroll = typeof window === "undefined" ? 0 : window.scrollY;
    state.camera.position.x = k.x * 0.55;
    state.camera.position.y = k.y * 0.35 - scroll * 0.0035;
    state.camera.lookAt(0, -scroll * 0.0035, 0);
  });

  return (
    <>
      <mesh frustumCulled={false} renderOrder={-1}>
        <planeGeometry args={[2, 2]} />
        <shaderMaterial ref={nebulaMat} vertexShader={NEBULA_VERT} fragmentShader={NEBULA_FRAG} uniforms={nebulaUniforms} depthWrite={false} depthTest={false} />
      </mesh>
      <points geometry={dustGeo} frustumCulled={false}>
        <shaderMaterial
          ref={dustMat}
          vertexShader={DUST_VERT}
          fragmentShader={DUST_FRAG}
          uniforms={dustUniforms}
          transparent
          depthWrite={false}
          blending={dark ? THREE.AdditiveBlending : THREE.NormalBlending}
        />
      </points>
    </>
  );
}

/** Drives rendering ourselves: capped fps, paused when hidden, one frame when motion is reduced. */
function Driver({ fps, animated }: { fps: number; animated: boolean }) {
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

class Boundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  render() {
    return this.state.failed ? null : this.props.children;
  }
}

export default function DeepField() {
  const [mode] = useFxMode();
  const reduced = useReducedMotion();
  const { resolvedTheme } = useTheme();
  if (mode === "off") return null;
  const dark = resolvedTheme !== "light";
  return (
    <div aria-hidden className="pointer-events-none fixed inset-0 -z-10">
      <Boundary>
        <Canvas
          dpr={mode === "lite" ? 1 : [1, 1.5]}
          camera={{ position: [0, 0, 6], fov: 50 }}
          frameloop="demand"
          gl={{ antialias: false, alpha: false, powerPreference: mode === "lite" ? "low-power" : "default" }}
        >
          <Driver fps={mode === "lite" ? 30 : 60} animated={!reduced} />
          <Field mode={mode} animated={!reduced} dark={dark} />
        </Canvas>
      </Boundary>
    </div>
  );
}
