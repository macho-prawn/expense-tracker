import express from "express";
import path from "path";
import { fileURLToPath } from "url";
import { createProxyMiddleware } from "http-proxy-middleware";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const distDir = path.resolve(__dirname, "../dist");
const serviceUrl = process.env.SERVICE_URL || "http://localhost:3001";
const port = Number(process.env.PORT || 8082);

const app = express();

app.disable("x-powered-by");

app.get("/healthz", (_req, res) => {
  res.status(200).json({ status: "ok" });
});

app.use(
  "/api",
  createProxyMiddleware({
    target: serviceUrl,
    changeOrigin: true,
    pathRewrite: {
      "^/api": ""
    }
  })
);

app.use(
  express.static(distDir, {
    index: false,
    extensions: ["html"]
  })
);

app.get("*", (_req, res) => {
  res.sendFile(path.join(distDir, "index.html"));
});

app.listen(port, () => {
  console.log(`ShareTab web listening on ${port}`);
});
