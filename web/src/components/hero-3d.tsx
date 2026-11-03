"use client";

import { Component, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Canvas, useFrame } from "@react-three/fiber";
import { useTheme } from "next-themes";
import * as THREE from "three";

export type OrbitNode = { id: string; status: string };

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

function Dot({ position, status }: { position: [number, number, number]; status: string }) {
  const ref = useRef<THREE.Mesh>(null);
  const live = LIVE.has(status);
  useFrame(({ clock }) => {
    if (!ref.current || !live) return;
    ref.current.scale.setScalar(1 + Math.sin(clock.elapsedTime * 3 + position[0]) * 0.35);
  });
  const color = STATUS_COLOR[status] ?? "#8a93a6";
  return (
    <mesh ref={ref} position={position}>
      <sphereGeometry args={[0.09, 16, 16]} />
      <meshStandardMaterial color={color} emissive={color} emissiveIntensity={live ? 1.1 : 0.45} />
    </mesh>
  );
}

function Scene({ nodes, reduced, brass }: { nodes: OrbitNode[]; reduced: boolean; brass: string }) {
  const tilt = useRef<THREE.Group>(null);
  const spin = useRef<THREE.Group>(null);
  const core = useRef<THREE.Mesh>(null);

  const placed = useMemo(() => {
    const shown = nodes.slice(0, 28);
    const n = Math.max(shown.length, 1);
    const golden = Math.PI * (3 - Math.sqrt(5));
    return shown.map((node, i) => {
      const phi = Math.acos(1 - (2 * (i + 0.5)) / n);
      const theta = golden * i;
      const r = 1.85 + (i % 3) * 0.32;
      return {
        id: node.id,
        status: node.status,
        pos: [r * Math.sin(phi) * Math.cos(theta), r * Math.cos(phi), r * Math.sin(phi) * Math.sin(theta)] as [number, number, number],
      };
    });
  }, [nodes]);

  useFrame((state, dt) => {
    if (reduced) return;
    if (spin.current) spin.current.rotation.y += dt * 0.16;
    if (core.current) {
      core.current.rotation.x += dt * 0.12;
      core.current.rotation.z += dt * 0.07;
    }
    if (tilt.current) {
      tilt.current.rotation.x = THREE.MathUtils.damp(tilt.current.rotation.x, -state.pointer.y * 0.3, 3, dt);
      tilt.current.rotation.y = THREE.MathUtils.damp(tilt.current.rotation.y, state.pointer.x * 0.4, 3, dt);
    }
  });

  return (
    <>
      <ambientLight intensity={0.7} />
      <pointLight position={[4, 3, 5]} intensity={40} color="#ffffff" />
      <group ref={tilt}>
        <group ref={spin}>
          <mesh ref={core}>
            <icosahedronGeometry args={[1.05, 1]} />
            <meshBasicMaterial color={brass} wireframe transparent opacity={0.85} />
          </mesh>
          <mesh>
            <icosahedronGeometry args={[0.52, 0]} />
            <meshStandardMaterial color={brass} emissive={brass} emissiveIntensity={0.55} flatShading />
          </mesh>
          <mesh rotation={[Math.PI / 2.3, 0.2, 0]}>
            <torusGeometry args={[2.35, 0.006, 8, 160]} />
            <meshBasicMaterial color={brass} transparent opacity={0.4} />
          </mesh>
          <mesh rotation={[Math.PI / 3.2, -0.6, 0.4]}>
            <torusGeometry args={[1.95, 0.005, 8, 160]} />
            <meshBasicMaterial color={brass} transparent opacity={0.25} />
          </mesh>
          {placed.map((p) => (
            <Dot key={p.id} position={p.pos} status={p.status} />
          ))}
        </group>
      </group>
    </>
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

/** Decorative scene: a brass wireframe core with one orbiting dot per live task, colored by status. */
export default function Hero3D({ nodes, className }: { nodes: OrbitNode[]; className?: string }) {
  const { resolvedTheme } = useTheme();
  const [reduced, setReduced] = useState(false);
  useEffect(() => {
    const mq = window.matchMedia("(prefers-reduced-motion: reduce)");
    setReduced(mq.matches);
    const on = () => setReduced(mq.matches);
    mq.addEventListener("change", on);
    return () => mq.removeEventListener("change", on);
  }, []);
  const brass = resolvedTheme === "light" ? "#a97b12" : "#e3b648";

  return (
    <div className={className} aria-hidden>
      <Boundary>
        <Canvas
          dpr={[1, 1.75]}
          camera={{ position: [0, 0, 6.4], fov: 38 }}
          frameloop={reduced ? "demand" : "always"}
          gl={{ antialias: true, alpha: true }}
        >
          <Scene nodes={nodes} reduced={reduced} brass={brass} />
        </Canvas>
      </Boundary>
    </div>
  );
}
