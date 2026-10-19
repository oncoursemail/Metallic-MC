const fs=require('fs');const vm=require('vm');const path=require('path');
const root=path.resolve(__dirname,'..');const source=fs.readFileSync(path.join(root,'app.js'),'utf8');const doc=fs.readFileSync(path.join(root,'index.html'),'utf8');
for(const concept of ['foundry','nocturne','fieldnote']){
 const map={};function el(){return{dataset:{},innerHTML:'',textContent:'',setAttribute(){},removeAttribute(){},classList:{toggle(){}}}}
 const d={querySelector(k){return map[k]??=el()},querySelectorAll(){return []},addEventListener(){}};
 vm.runInNewContext(source,{document:d,location:{hash:'#overview'},history:{replaceState(){}},window:{scrollTo(){}},clearTimeout,setTimeout,requestAnimationFrame:()=>{}}, {filename:'app.js'});
 let html=doc.replace('<main id="main" tabindex="-1" aria-live="off"></main>','<main id="main" tabindex="-1" aria-live="off">'+map['#main'].innerHTML+'</main>');
 html=html.replace('<div id="app" class="app-shell" data-concept="foundry"',`<div id="app" class="app-shell" data-concept="${concept}"`).replace('<script src="./app.js" defer></script>','');
 const dir=path.join(root,'_renders');fs.mkdirSync(dir,{recursive:true});fs.writeFileSync(path.join(dir,concept+'.html'),html);console.log(concept,html.length);
}
