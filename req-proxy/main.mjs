import { spawn } from "child_process";
import path from "path";
import os from "os";
import { reqProxy } from "./req-proxy.mjs";

function getGoBinary() {
    const platform = os.platform();
    if (platform === "win32") return path.resolve("./server-windows.exe");
    if (platform === "linux") return path.resolve("./server-linux");
    if (platform === "android") return path.resolve("./server-android"); // 安卓设备
    throw new Error("Unsupported platform: " + platform);
}

const goBinary = getGoBinary();

// 可以通过 Node 传入端口号
const port = process.argv[2] || "57571";

console.log("Starting Go server:", goBinary, "on port", port);

const proc = spawn(goBinary, ["-p", port], {
    stdio: ["ignore", "pipe", "pipe"],
});

// 打印 Go 服务输出
proc.stdout.on("data", (data) => {
    console.log("[Go-Server]", data.toString().trim());
});
proc.stderr.on("data", (data) => {
    console.error("[Go-Server-ERR]", data.toString().trim());
});

// 确保 Node 退出时，杀掉 Go 服务
process.on("exit", () => {
    proc.kill();
});
process.on("SIGINT", () => process.exit());
process.on("SIGTERM", () => process.exit());

console.log("Node wrapper started, Go server should be running...");


(async () => {
    await new Promise((resolve) => {
        setTimeout(() => {
            resolve();
        }, 2000);
    });
    try {
        const res = await reqProxy({
            method: "GET",
            url: "https://self-signed.badssl.com/",
            headers: { "User-Agent": "okhttp/4.19" },
            timeout: 10000,
        });

        console.log("Status:", res.status);
        console.log("Headers:", res.headers);
        console.log("Body length:", res.body.length);
        console.log("Body:", res.body);
    } catch (err) {
        console.error(err);
    }
})();
