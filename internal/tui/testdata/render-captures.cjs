// Render real Bubble Tea frames with the bundled xterm terminal emulator.
// ONLCR below reproduces the PTY's LF -> CRLF output processing.
const fs=require('fs'),path=require('path');
const modules=process.argv[3];
const directory=process.argv[2];
if(!modules||!directory)throw new Error('Usage: node render-captures.cjs CAPTURE_DIRECTORY NODE_MODULES');
const {chromium}=require(path.join(modules,'playwright'));
(async()=>{
const browser=await chromium.launch({headless:true});
const assets=path.join(modules,'playwright-core/lib/vite/traceViewer');
const module=fs.readFileSync(path.join(assets,'assets/'+fs.readdirSync(path.join(assets,'assets')).find(x=>/^xtermModule.*\.js$/.test(x))),'utf8').replace(/^import[^;]+;/, 'function e(cb){let cache;return()=>{if(!cache){cache={exports:{}};cb(cache.exports,cache)}return cache.exports}}').replace('export{a as default};','window.Terminal=a.Terminal;');
const css=fs.readFileSync(path.join(assets,fs.readdirSync(assets).find(x=>/^xtermModule.*\.css$/.test(x))),'utf8');
for(const file of fs.readdirSync(directory).filter(x=>x.endsWith('.ansi'))){
const [,cols,rows]=file.match(/(\d+)x(\d+)/);const page=await browser.newPage({viewport:{width:Number(cols)*10+40,height:Number(rows)*20+40},deviceScaleFactor:1});
await page.setContent('<html><style>'+css+'body{background:#121212;margin:20px}</style><div id="terminal"></div></html>');
await page.addScriptTag({content:module});
await page.evaluate(async({cols,rows,stream})=>{const term=new window.Terminal({cols,rows,fontSize:16,fontFamily:'Menlo,monospace',lineHeight:1.1,theme:{background:'#121212',foreground:'#dddddd'},allowProposedApi:true});term.open(document.querySelector('#terminal'));await new Promise(r=>term.write(stream,r));}, {cols:Number(cols),rows:Number(rows),stream:fs.readFileSync(directory+'/'+file,'utf8').replace(/(?<!\r)\n/g,'\r\n')});
await page.screenshot({path:directory+'/'+file.replace('.ansi','.png')});await page.close();
}
await browser.close();})();
