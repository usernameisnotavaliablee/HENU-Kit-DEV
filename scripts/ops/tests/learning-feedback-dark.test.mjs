import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// 学习反馈（LF-01..LF-07）的「默认全暗 + fail-closed 回退」在本仓是被测试守住的
// 合同，不是口头承诺。任何切流（#166）都必须是一次会让本文件变红的显式改动，
// 而不是某个部署面悄悄把开关写成 1。
const root = new URL("../../../", import.meta.url);
const read = (relative) => readFileSync(new URL(relative, root), "utf8");

const GATEWAY_FLAG = "PORTAL_ENABLE_QUIZCRAFT_LEARNING_REPORTS";
const BROWSER_FLAG = "NEXT_PUBLIC_PORTAL_ENABLE_QUIZCRAFT_LEARNING_REPORTS";
const WORKER_FLAG = "QUIZCRAFT_LEARNING_WORKER_ENABLED";
const LIMIT_FLAG = "QUIZCRAFT_LEARNING_MANUAL_LIMIT";
const LEARNING_FLAGS = [GATEWAY_FLAG, BROWSER_FLAG, WORKER_FLAG];
const LEARNING_BANK = "quizcraft_learning";

test("每个部署面都保持学习反馈暗态，且没有一面把它写死成开启", () => {
  const example = read(".env.henukit.example");
  assert.match(example, new RegExp(`^${GATEWAY_FLAG}=0$`, "m"));
  assert.match(example, new RegExp(`^${BROWSER_FLAG}=0$`, "m"));
  assert.match(example, new RegExp(`^${WORKER_FLAG}=0$`, "m"));
  // 限流是成本守卫，不是开关：默认值必须仍在，否则暗态部署会失去滥用保护。
  assert.match(example, new RegExp(`^${LIMIT_FLAG}=10$`, "m"));

  const compose = read("docker-compose.henukit.yml");
  assert.match(compose, new RegExp(`${GATEWAY_FLAG}: \\$\\{${GATEWAY_FLAG}:-0\\}`));
  assert.match(compose, new RegExp(`${BROWSER_FLAG}: \\$\\{${BROWSER_FLAG}:-0\\}`));
  assert.match(compose, new RegExp(`${WORKER_FLAG}: \\$\\{${WORKER_FLAG}:-0\\}`));
  assert.match(compose, new RegExp(`${LIMIT_FLAG}: \\$\\{${LIMIT_FLAG}:-10\\}`));

  // 生产 overlay 只覆盖必要项，学习开关一律继承 base compose 的暗态；它自己不许开启。
  const overlay = read("docker-compose.henukit.prebuilt.yml");
  for (const flag of LEARNING_FLAGS) {
    assert.doesNotMatch(overlay, new RegExp(`${flag}[^\\n]*[:=]\\s*(?:\\$\\{[^}]*:-)?1\\b`), `overlay turned on ${flag}`);
  }

  // 浏览器开关是构建期烘焙的：镜像默认 0，且只能由同名 build arg 传入。
  const dockerfile = read("apps/portal/Dockerfile");
  assert.match(dockerfile, new RegExp(`ARG ${BROWSER_FLAG}=0`));
  assert.match(dockerfile, new RegExp(`ENV ${BROWSER_FLAG}=\\$${BROWSER_FLAG}`));

  // 发布镜像脚本写出的 release env 目前只烘焙 catalog/V2 读取；学习反馈必须等 #166，
  // 这里既锚定「读的是这段 release env」，也锁死「不许顺手烘焙成 1」。
  const releaseImages = read("scripts/ops/henukit-release-images.sh");
  assert.match(releaseImages, /NEXT_PUBLIC_PORTAL_ENABLE_QUIZCRAFT_CATALOG=1/);
  for (const flag of LEARNING_FLAGS) {
    assert.doesNotMatch(releaseImages, new RegExp(`${flag}=1`), `release images must stay dark for ${flag}`);
  }

  // 每面都必须真的提到学习反馈，避免「文件被挪走后测试静默通过」。
  for (const flag of [GATEWAY_FLAG, BROWSER_FLAG, WORKER_FLAG]) {
    assert.ok(example.includes(flag), `.env.henukit.example 缺少 ${flag}`);
  }
  assert.ok(compose.includes(LEARNING_BANK) || compose.includes(GATEWAY_FLAG));
});

test("读侧默认值是 fail-closed，而不是「不设就等于开」", () => {
  const gatewayConfig = read("services/portal-gateway/internal/config/config.go");
  assert.match(gatewayConfig, new RegExp(`getenv\\("${GATEWAY_FLAG}"\\)`));
  // 网关的运行时门禁：置 1 但没开 V2 读取就必须启动失败，而不是半开。
  assert.match(gatewayConfig, /requires PORTAL_ENABLE_QUIZCRAFT_V2_READS=1/);

  const portalEnv = read("apps/portal/src/lib/api/env.ts");
  assert.match(portalEnv, new RegExp(`${BROWSER_FLAG}\\s*===\\s*"1"`));
});

test("回退顺序在运维矩阵里仍有记录：先关会员入口，再关 worker，最后关调度", () => {
  const matrix = read("docs/operations/practice-wiring-matrix.md");
  const steps = [
    `${GATEWAY_FLAG}=0`,
    "浏览器开关烘焙 0 并重建 Portal",
    `${WORKER_FLAG}=0`,
    "QUIZCRAFT_LEARNING_SCHEDULER_INTERVAL=0",
  ];
  const positions = steps.map((step) => matrix.indexOf(step));
  positions.forEach((position, index) => assert.notEqual(position, -1, `矩阵缺少回退步骤：${steps[index]}`));
  for (let index = 1; index < positions.length; index += 1) {
    assert.ok(positions[index - 1] < positions[index], `回退顺序被改动：${steps[index - 1]} 必须在 ${steps[index]} 之前`);
  }
});
