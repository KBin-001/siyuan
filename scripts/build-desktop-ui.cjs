const fs = require("node:fs");
const path = require("node:path");

// 独立输出构建，不清理或写入开发服务器使用的 app/stage/build。
async function buildDesktopUI(runtimeRoot) {
    const appRoot = path.resolve(__dirname, "../app");
    const outputRoot = path.resolve(runtimeRoot, "stage/build");
    const allowedRoot = path.resolve(__dirname, "../.cache/windows-desktop/runtime/stage/build");
    if (outputRoot !== allowedRoot) {
        throw new Error("Unexpected desktop output directory");
    }
    const webpack = require(path.join(appRoot, "node_modules/webpack"));
    const {CleanWebpackPlugin} = require(path.join(appRoot, "node_modules/clean-webpack-plugin"));
    process.chdir(appRoot);
    for (const [name, file] of [["app", "webpack.config.js"], ["desktop", "webpack.desktop.js"],
        ["mobile", "webpack.mobile.js"], ["export", "webpack.export.js"]]) {
        console.log(`Building UI: ${name}`);
        const config = require(path.join(appRoot, file))({}, {mode: "production"});
        config.watch = false;
        config.output.path = path.join(outputRoot, name);
        // 原配置的清理目标是源码目录中的绝对路径，独立构建必须移除该清理步骤。
        config.plugins = config.plugins.filter(plugin => !(plugin instanceof CleanWebpackPlugin));
        fs.mkdirSync(config.output.path, {recursive: true});
        await new Promise((resolve, reject) => {
            const compiler = webpack(config);
            compiler.run((error, stats) => {
                compiler.close((closeError) => {
                    if (error || closeError) return reject(error || closeError);
                    console.log(stats.toString({preset: "errors-warnings", colors: false}));
                    if (stats.hasErrors()) return reject(new Error(`${name} UI build failed`));
                    resolve();
                });
            });
        });
    }
}

if (require.main === module) {
    if (!process.argv[2]) throw new Error("Missing desktop runtime directory");
    buildDesktopUI(process.argv[2]).catch(error => {
        console.error(error);
        process.exitCode = 1;
    });
}

module.exports = {buildDesktopUI};
