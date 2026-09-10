using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.Net;
using System.Text;
using System.Threading;
using System.Threading.Tasks;
using System.Web.Script.Serialization;

namespace KairiseiLauncher
{
    internal sealed class OperationResult
    {
        public int exit_code { get; set; }
        public string output { get; set; }
        public string diagnostic_path { get; set; }
    }

    // The UI consumes operations and snapshots. Scripts remain the single
    // lifecycle authority for both the launcher and command-line entry points.
    internal sealed class ServerController
    {
        private readonly string root;
        private readonly SemaphoreSlim operation = new SemaphoreSlim(1, 1);

        public ServerController(string packageRoot) { root = Path.GetFullPath(packageRoot); }

        public Task<OperationResult> StartAsync(string host, int port, bool validationOnly)
        {
            var values = new Dictionary<string, object> { { "AdvertiseHost", host }, { "Port", port } };
            if (validationOnly) values.Add("ValidationOnly", true);
            return ExecuteAsync("Start-Server.ps1", values, 90);
        }

        public Task<OperationResult> StopAsync()
        {
            return ExecuteAsync("Stop-Server.ps1", new Dictionary<string, object>(), 25);
        }

        internal async Task<OperationResult> ExecuteAsync(string scriptName, IDictionary<string, object> values, int timeoutSeconds)
        {
            if (!await operation.WaitAsync(0))
                return new OperationResult { exit_code = 5, output = "另一项服务操作正在执行，请稍候。" };
            try { return await Task.Run(() => Execute(scriptName, values, timeoutSeconds)); }
            finally { operation.Release(); }
        }

        private OperationResult Execute(string scriptName, IDictionary<string, object> values, int timeoutSeconds)
        {
            var script = Path.GetFullPath(Path.Combine(root, scriptName));
            if (!String.Equals(Path.GetDirectoryName(script), root.TrimEnd(Path.DirectorySeparatorChar), StringComparison.OrdinalIgnoreCase) || !File.Exists(script))
                return new OperationResult { exit_code = 2, output = "控制脚本缺失或不属于当前服务包。" };
            var directory = Path.Combine(root, "_local", "runtime", "launcher-operations", Guid.NewGuid().ToString("N"));
            Directory.CreateDirectory(directory);
            var resultPath = Path.Combine(directory, "result.json");
            var parameters = new List<string> { "'Confirm'=$false" };
            foreach (var pair in values)
            {
                var value = pair.Value is bool ? ((bool)pair.Value ? "$true" : "$false") :
                    Literal(Convert.ToString(pair.Value, System.Globalization.CultureInfo.InvariantCulture));
                parameters.Add(Literal(pair.Key) + "=" + value);
            }
            // No redirected OS pipes cross the detached-server boundary. The
            // short-lived command publishes its own UTF-8 result before exit.
            var command = "$ProgressPreference='SilentlyContinue'; $ErrorActionPreference='Stop'; $LASTEXITCODE=0; " +
                "$parameters=@{" + String.Join(";", parameters) + "}; $code=0; $text=''; " +
                "try { $text=(& " + Literal(script) + " @parameters | Out-String); $code=$LASTEXITCODE } " +
                "catch { $code=1; $text=($_ | Out-String) }; " +
                "$result=@{exit_code=$code;output=$text}; " +
                "[IO.File]::WriteAllText(" + Literal(resultPath) + ",($result | ConvertTo-Json -Compress),[Text.UTF8Encoding]::new($false)); exit $code";
            var powerShell = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.System), "WindowsPowerShell", "v1.0", "powershell.exe");
            var info = new ProcessStartInfo
            {
                FileName = powerShell,
                Arguments = "-NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand " + Convert.ToBase64String(Encoding.Unicode.GetBytes(command)),
                WorkingDirectory = root, UseShellExecute = false, CreateNoWindow = true
            };
            try
            {
                using (var process = Process.Start(info))
                {
                    if (process == null) throw new InvalidOperationException("无法启动 Windows PowerShell。");
                    if (!process.WaitForExit(timeoutSeconds * 1000))
                    {
                        try { process.Kill(); } catch { }
                        return SaveFailure(resultPath, 4, "控制操作超时；服务状态由独立检测确认，请查看日志。");
                    }
                    if (!File.Exists(resultPath) || new FileInfo(resultPath).Length > 2 * 1024 * 1024)
                        return SaveFailure(resultPath, 3, "控制脚本未返回有效结果，退出码 " + process.ExitCode + "。请查看服务日志。");
                    var result = new JavaScriptSerializer().Deserialize<OperationResult>(File.ReadAllText(resultPath, Encoding.UTF8));
                    if (result == null || result.exit_code != process.ExitCode)
                        throw new InvalidDataException("控制脚本结果与退出码不一致。");
                    result.diagnostic_path = directory;
                    return result;
                }
            }
            catch (Exception ex) { return SaveFailure(resultPath, 3, ex.Message); }
        }

        private static OperationResult SaveFailure(string path, int code, string message)
        {
            var result = new OperationResult { exit_code = code, output = message, diagnostic_path = Path.GetDirectoryName(path) };
            // Preserve a script's result if one already exists, including a
            // late completion after a controller deadline.
            var failurePath = Path.Combine(Path.GetDirectoryName(path), "controller-error.json");
            File.WriteAllText(failurePath, new JavaScriptSerializer().Serialize(result), new UTF8Encoding(false));
            return result;
        }

        private static string Literal(string value) { return "'" + (value ?? "").Replace("'", "''") + "'"; }

        public Task<ServerState> ReadStateAsync() { return Task.Run(() => ReadState()); }

        private ServerState ReadState()
        {
            var path = Path.Combine(root, "_local", "data", "server-state.json");
            if (!File.Exists(path)) return null;
            try
            {
                var state = new JavaScriptSerializer().Deserialize<ServerState>(File.ReadAllText(path, Encoding.UTF8));
                if (state == null || state.process_id <= 0 || (state.status != "RUNNING" && state.status != "STARTING")) return null;
                using (var process = Process.GetProcessById(state.process_id))
                {
                    if (!String.Equals(process.MainModule.FileName, Path.Combine(root, "kairi-server.exe"), StringComparison.OrdinalIgnoreCase)) return null;
                }
                state.IsReady = Healthy(state.port, "/healthz") && Healthy(state.admin_port, "/api/health");
                return state;
            }
            catch { return null; }
        }

        private static bool Healthy(int port, string path)
        {
            if (port < 1 || port > 65535) return false;
            try
            {
                var request = (HttpWebRequest)WebRequest.Create("http://127.0.0.1:" + port + path);
                request.Proxy = null;
                request.Timeout = 1200;
                request.ReadWriteTimeout = 1200;
                using (var response = request.GetResponse())
                using (var reader = new StreamReader(response.GetResponseStream(), Encoding.UTF8))
                {
                    var health = new JavaScriptSerializer().Deserialize<Dictionary<string, object>>(reader.ReadToEnd());
                    return health != null && health.ContainsKey("state") && Convert.ToString(health["state"]) == "PASS";
                }
            }
            catch { return false; }
        }
    }
}
