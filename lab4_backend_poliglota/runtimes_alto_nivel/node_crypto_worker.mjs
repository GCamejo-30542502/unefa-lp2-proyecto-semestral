import { pbkdf2Sync } from "node:crypto";
import { Worker, isMainThread, parentPort, workerData } from "node:worker_threads";

if (isMainThread) {
  const worker = new Worker(new URL(import.meta.url), {
    workerData: {
      password: "clave-demo-no-usar-en-produccion",
      salt: "sal-fija-solo-para-demostracion",
      iterations: 300_000,
      keyLength: 32,
    },
  });
  let eventLoopTicks = 0;
  const timer = setInterval(() => {
    eventLoopTicks += 1;
    console.log(`Event Loop principal activo (tick ${eventLoopTicks})`);
  }, 50);

  worker.on("message", ({ digest, iterations }) => {
    console.log(`PBKDF2-SHA-256 terminado en el worker (${iterations} iteraciones).`);
    console.log(`Digest hexadecimal: ${digest}`);
  });
  worker.on("error", (error) => {
    console.error("Fallo en el worker criptografico:", error);
    process.exitCode = 1;
  });
  worker.on("exit", (code) => {
    clearInterval(timer);
    if (code !== 0) {
      console.error(`El worker termino con codigo ${code}.`);
      process.exitCode = 1;
    }
    console.log(`El Event Loop principal siguio respondiendo (${eventLoopTicks} ticks).`);
  });
} else {
  const { password, salt, iterations, keyLength } = workerData;
  const digest = pbkdf2Sync(password, salt, iterations, keyLength, "sha256").toString("hex");
  parentPort.postMessage({ digest, iterations });
}
