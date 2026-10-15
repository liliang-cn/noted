
/* ================= framework ================= */
const device=$('#device'),views=$('#views'),ov=$('#ov');
const RM=matchMedia('(prefers-reduced-motion: reduce)').matches;
let SCALE=1,busy=false,curTab='today',noClick=0;
const TABS=['today','cal','plan','notes'];
const stacks={},tvs={};
const SC={},ACT={},IN={};
TABS.forEach(t=>{stacks[t]=[];const v=document.createElement('div');v.className='tabview';v.dataset.tab=t;views.appendChild(v);tvs[t]=v});

function mk(kind,p,tab){const scr={kind,p:p||{},st:{},tab:tab||curTab,el:document.createElement('section')};scr.el.className='screen'+(SC[kind].cls?' '+SC[kind].cls:'');scr.el._scr=scr;if(SC[kind].init)SC[kind].init(scr.p,scr);return scr}
const cur=()=>{const s=stacks[curTab];return s[s.length-1]};
const titleOf=scr=>{const d=SC[scr.kind];return typeof d.title==='function'?d.title(scr.p,scr):d.title};
const tgt=()=>{const s=cur();const d=SC[s.kind];return d.tgt?d.tgt(s.p,s):null};

function tabbarHTML(tab){
  const b=(t,l,i)=>`<button class="${t===tab?'on':''}" data-act="tab" data-tab="${t}">${ic(i,26)}<span>${l}</span></button>`;
  return `<nav class="tabbar">${b('today','今天','sun')}${b('cal','日历','cal')}<button class="plus" data-act="new" aria-label="新建"><i>${ic('plus',24)}</i></button>${b('plan','计划','target')}${b('notes','笔记','note')}</nav>`;
}
function render(scr){
  const d=SC[scr.kind];const st=stacks[scr.tab];const i=st.indexOf(scr);if(i<0)return;
  if(d.gone&&d.gone(scr.p))return;
  const prev=st[i-1];
  const old=scr.el.querySelector('.scroll');const top=old?old.scrollTop:0;
  const hx={};if(old)$$('[data-hx]',scr.el).forEach(x=>hx[x.dataset.hx]=x.scrollLeft);
  const title=titleOf(scr);
  let pt=prev?titleOf(prev):'';if(pt.length>6)pt='返回';
  const left=prev?`<button class="back" data-act="back">${ic('back',22)}<span>${esc(pt)}</span></button>`:(d.left?d.left(scr.p,scr):'');
  const center=d.center?d.center(scr.p,scr):`<span class="nav-title">${esc(title)}</span>`;
  const right=d.right?d.right(scr.p,scr):'';
  const body=d.body(scr.p,scr);
  const bottom=d.bottom?d.bottom(scr.p,scr):'';
  scr.el.classList.toggle('root',i===0);if(d.white)scr.el.classList.toggle('white',!!d.white(scr.p,scr));scr.el.classList.toggle('has-bottom',!!bottom);
  const lt=d.large?`<div class="lt"><h1>${esc(title)}</h1>${d.sub?`<div class="lt-sub">${d.sub(scr.p,scr)}</div>`:''}</div>`:'';
  scr.el.innerHTML=`<header class="nav ${d.large?'large':''} ${d.navcls||''}"><div class="nav-bar"><div class="nav-l">${left}</div><div class="nav-c">${center}</div><div class="nav-r">${right}</div></div></header><div class="scroll">${lt}${body}</div>${bottom?`<div class="bottombar ${d.bcls?d.bcls(scr.p,scr):''}">${bottom}</div>`:''}${i===0?tabbarHTML(scr.tab):''}${i>0?'<div class="edge"></div>':''}`;
  scr.st.hint=false;
  const sc=scr.el.querySelector('.scroll');
  if(scr.st.top!=null){sc.scrollTop=scr.st.top;scr.st.top=null}else sc.scrollTop=top;
  $$('[data-hx]',scr.el).forEach(x=>{if(hx[x.dataset.hx])x.scrollLeft=hx[x.dataset.hx]});
  navState(scr);post(scr);if(d.mount)d.mount(scr.p,scr);
}
function post(scr){
  $$('.wcol',scr.el).forEach(c=>{c._prog=true;c.scrollTop=(+c.dataset.i)*36;setTimeout(()=>c._prog=false,60)});
  $$('textarea.ag',scr.el).forEach(grow);
  $$('[data-to]',scr.el).forEach(c=>{requestAnimationFrame(()=>requestAnimationFrame(()=>{c.style.strokeDashoffset=c.dataset.to}))});
  $$('.slider',scr.el).forEach(s=>s.style.setProperty('--v',s.value+'%'));
}
function grow(t){t.style.height='auto';t.style.height=(t.scrollHeight+2)+'px'}
function navState(scr){const sc=scr.el.querySelector('.scroll'),nav=scr.el.querySelector('.nav');if(!sc||!nav)return;const y=sc.scrollTop;if(SC[scr.kind].large)nav.classList.toggle('collapsed',y>42);else nav.classList.toggle('scrolled',y>2)}
function refreshAll(skipCur){
  TABS.forEach(t=>{const st=stacks[t];
    for(let i=st.length-1;i>0;i--){const s=st[i];const d=SC[s.kind];if(d.gone&&d.gone(s.p)){if(t===curTab&&i===st.length-1&&!skipCur){}s.el.remove();st.splice(i,1);const n=st[st.length-1];n.el.style.transform='';n.el.style.visibility='';n.el.classList.remove('under')}}
    st.forEach(s=>{if(skipCur&&s===cur())return;render(s)})});
  S._anim=null;
}
let softT;function soft(){clearTimeout(softT);softT=setTimeout(()=>refreshAll(true),350)}

function push(kind,p){
  if(busy)return;closeSw();closeAllOv();
  const st=stacks[curTab];const prev=st[st.length-1];
  const scr=mk(kind,p,curTab);st.push(scr);tvs[curTab].appendChild(scr.el);render(scr);
  const af=scr.el.querySelector('[data-autofocus]');if(af){try{af.focus({preventScroll:true})}catch(e){af.focus()}}
  if(RM){prev.el.style.visibility='hidden';return scr}
  busy=true;scr.el.classList.add('pushed');scr.el.style.transform='translateX(100%)';scr.el.getBoundingClientRect();
  scr.el.classList.add('anim');prev.el.classList.add('anim','under');
  scr.el.style.transform='translateX(0)';prev.el.style.transform='translateX(-30%)';
  setTimeout(()=>{scr.el.classList.remove('anim');prev.el.classList.remove('anim');if(stacks[curTab].includes(scr)&&cur()===scr)prev.el.style.visibility='hidden';busy=false},470);
  return scr;
}
function pop(n){
  n=n||1;const st=stacks[curTab];if(st.length<2||(busy&&!edS))return;
  closeSw();closeAllOv();
  if(document.activeElement&&document.activeElement.blur)document.activeElement.blur();
  const top=st.pop();if(SC[top.kind].leave)SC[top.kind].leave(top.p,top);
  for(let k=1;k<n&&st.length>1;k++){const x=st.pop();x.el.remove()}
  const prev=st[st.length-1];prev.el.style.visibility='';render(prev);
  if(RM){top.el.remove();prev.el.style.transform='';prev.el.classList.remove('under');return}
  busy=true;top.el.classList.add('anim');prev.el.classList.add('anim');
  top.el.getBoundingClientRect();
  top.el.style.transform='translateX(100%)';prev.el.style.transform='translateX(0)';prev.el.classList.remove('under');
  setTimeout(()=>{top.el.remove();prev.el.classList.remove('anim');prev.el.style.transform='';busy=false},470);
}
function setTab(t){
  if(busy)return;closeSw();closeAllOv();
  if(t===curTab){const st=stacks[t];if(st.length>1)pop(st.length-1);else{const sc=st[0].el.querySelector('.scroll');sc.scrollTo({top:0,behavior:RM?'auto':'smooth'})}return}
  tvs[curTab].classList.remove('on');curTab=t;tvs[t].classList.add('on');render(cur());
}
function openRef(ref){const map={t:'todo',e:'event',n:'note',p:'project',g:'goal',h:'holding'};if(!get(ref))return hud('已删除','x');push(map[ref[0]],{id:ref.slice(2)})}

/* ================= overlays ================= */
function openOv(html,cls){const w=document.createElement('div');w.className='ovl '+(cls||'');w.innerHTML=html;ov.appendChild(w);w._fns=[];return w}
function closeOv(w){if(!w||w._closing)return;w._closing=true;w.classList.add('out');setTimeout(()=>w.remove(),RM?0:240)}
function closeAllOv(){$$('.ovl:not(.h)',ov).forEach(closeOv)}
function relRect(el){const a=el.getBoundingClientRect(),b=device.getBoundingClientRect();return{left:(a.left-b.left)/SCALE,top:(a.top-b.top)/SCALE,right:(a.right-b.left)/SCALE,bottom:(a.bottom-b.top)/SCALE,width:a.width/SCALE,height:a.height/SCALE}}
function menu(anchor,items,title){
  const html=items.map((it,i)=>it==='-'?'<div class="mg"></div>':`<button data-act="ovf" data-i="${i}" class="${it.danger?'danger':''}"><span class="mck">${it.checked?ic('check',16):''}</span><span class="ml">${it.dot?dot(it.dot):''}${esc(it.label)}</span>${it.icon?ic(it.icon,20):''}</button>`).join('');
  const w=openOv(`<div class="scrim" data-act="ovx"></div><div class="menu">${title?`<div class="mt">${esc(title)}</div>`:''}${html}</div>`,'m');
  w._fns=items.map(it=>it.fn);
  const m=w.querySelector('.menu'),r=relRect(anchor),DW=device.clientWidth,DH=device.clientHeight,W=250,H=m.offsetHeight;
  const leftSide=r.left+r.width/2<DW/2;let left=leftSide?r.left:r.right-W;left=Math.max(8,Math.min(left,DW-W-8));
  let top=r.bottom+6,o='top';if(top+H>DH-16){top=r.top-H-6;o='bottom'}top=Math.max(8,top);
  m.style.left=left+'px';m.style.top=top+'px';m.style.transformOrigin=o+' '+(leftSide?'left':'right');
  return w;
}
function actionSheet(o){
  const acts=o.actions.map((a,i)=>`<button data-act="ovf" data-i="${i}" class="${a.danger?'danger':''}">${esc(a.label)}</button>`).join('');
  const w=openOv(`<div class="dim" data-act="ovx"></div><div class="asheet"><div class="g">${o.title||o.msg?`<div class="hd">${o.title?`<b>${esc(o.title)}</b>`:''}${o.msg?esc(o.msg):''}</div>`:''}${acts}</div><div class="g"><button class="cancel" data-act="ovx">取消</button></div></div>`,'a');
  w._fns=o.actions.map(a=>a.fn);return w;
}
function alertBox(o){
  const acts=o.actions.map((a,i)=>`<button data-act="ovf" data-i="${i}" class="${a.danger?'danger':''} ${a.bold?'bold':''}">${esc(a.label)}</button>`).join('');
  const w=openOv(`<div class="dim"></div><div class="alert"><div class="b"><h4>${esc(o.title)}</h4>${o.msg?`<p>${esc(o.msg)}</p>`:''}</div><div class="acts">${acts}</div></div>`,'al');
  w._fns=o.actions.map(a=>a.fn);return w;
}
function confirmDel(title,msg,fn,label){alertBox({title,msg,actions:[{label:'取消'},{label:label||'删除',danger:true,bold:true,fn}]})}
function sheet(o){
  const w=openOv(`<div class="dim" data-act="ovx"></div><div class="fsheet"><div class="grab"></div><div class="fs-h"><button class="navbtn" data-act="ovx">取消</button><b>${esc(o.title)}</b><button class="navbtn b" data-act="fsdone">${esc(o.done||'完成')}</button></div><div class="fs-b">${o.body}</div></div>`,'s');
  w._done=o.onDone;if(o.mount)o.mount(w);
  const f=w.querySelector('[data-autofocus]');if(f){try{f.focus({preventScroll:true})}catch(e){f.focus()}if(f.select)f.select()}
  return w;
}
function hud(text,icon){$$('.ovl.h',ov).forEach(x=>x.remove());const w=openOv(`<div class="hud">${ic(icon||'check',18)}<span>${esc(text)}</span></div>`,'h');setTimeout(()=>closeOv(w),1700)}
ACT.ovx=t=>closeOv(t.closest('.ovl'));
ACT.ovf=t=>{const w=t.closest('.ovl');const fn=w._fns[+t.dataset.i];closeOv(w);if(fn)setTimeout(fn,RM?0:60)};
ACT.fsdone=t=>{const w=t.closest('.ovl');const r=w._done?w._done(w):true;if(r!==false){if(document.activeElement)document.activeElement.blur();closeOv(w)}};
ACT.chip=t=>{const p=t.parentNode;$$('.chip',p).forEach(c=>c.classList.toggle('on',c===t))};
ACT.segl=t=>{const p=t.parentNode;$$('button',p).forEach(c=>c.classList.toggle('on',c===t));if(p.dataset.cb&&ACT[p.dataset.cb])ACT[p.dataset.cb](t)};

/* ================= events ================= */
device.addEventListener('click',e=>{
  if(Date.now()<noClick){e.preventDefault();e.stopPropagation();return}
  const t=e.target.closest('[data-act]');if(!t||t.disabled)return;
  const f=ACT[t.dataset.act];if(f){f(t,e)}
},true);
device.addEventListener('input',e=>{const t=e.target;const k=t.dataset&&t.dataset.in;if(k&&IN[k])IN[k](t,e)});
device.addEventListener('keydown',e=>{const t=e.target;if(e.key==='Enter'&&t.classList&&t.classList.contains('big-in')){e.preventDefault();if(S.draft.title.trim())ACT.add();else t.blur()}});
device.addEventListener('scroll',e=>{const t=e.target;if(!t.classList)return;if(t.classList.contains('scroll')){const s=t.closest('.screen');if(s&&s._scr)navState(s._scr);if(openSw)closeSw()}else if(t.classList.contains('wcol'))wheelScroll(t)},true);
document.addEventListener('touchstart',()=>{},{passive:true});
$$('.kbbar').forEach(()=>{});
device.addEventListener('mousedown',e=>{if(e.target.closest('.kbbar .k,.atp'))e.preventDefault()});

/* swipe rows / edge back / horizontal swipe */
let swS=null,openSw=null,edS=null,hS=null;
function closeSw(){if(!openSw)return;const r=openSw;openSw=null;const c=r.querySelector('.sw-c');c.style.transform='';setTimeout(()=>r.classList.remove('dl','dr'),350)}
device.addEventListener('pointerdown',e=>{
  if(openSw&&!e.target.closest('.sw-a')){closeSw();noClick=Date.now()+400;return}
  const ed=e.target.closest('.edge');
  if(ed&&!busy&&stacks[curTab].length>1){const st=stacks[curTab];edS={x0:e.clientX,top:st[st.length-1],prev:st[st.length-2],w:device.clientWidth,id:e.pointerId,t0:performance.now(),dx:0};try{ed.setPointerCapture(e.pointerId)}catch(_){}
    edS.prev.el.style.visibility='';return}
  const c=e.target.closest('.sw-c');
  if(c&&(e.button===0||e.pointerType!=='mouse')&&!e.target.closest('input,textarea'))swS={c,row:c.parentNode,x0:e.clientX,y0:e.clientY,on:false,id:e.pointerId,x:0};
  const h=e.target.closest('[data-hsw]');if(h)hS={h,x0:e.clientX,y0:e.clientY,id:e.pointerId};
});
device.addEventListener('pointermove',e=>{
  if(edS&&e.pointerId===edS.id){const dx=Math.max(0,(e.clientX-edS.x0)/SCALE);edS.dx=dx;edS.top.el.style.transform=`translateX(${dx}px)`;edS.prev.el.style.transform=`translateX(${-.3*edS.w+.3*dx}px)`;return}
  if(swS&&e.pointerId===swS.id){
    const dx=(e.clientX-swS.x0)/SCALE,dy=(e.clientY-swS.y0)/SCALE;
    if(!swS.on){if(Math.abs(dx)>10&&Math.abs(dx)>Math.abs(dy)*1.3){swS.on=true;swS.row.classList.add('drag');try{swS.c.setPointerCapture(e.pointerId)}catch(_){}}else if(Math.abs(dy)>10){swS=null;return}}
    if(swS.on){const L=swS.row.dataset.l,R=swS.row.dataset.r;let x=dx;if(x>0&&!L)x*=.12;if(x<0&&!R)x*=.12;swS.x=x;swS.c.style.transform=`translateX(${x}px)`;swS.row.classList.toggle('dl',x>0);swS.row.classList.toggle('dr',x<0);e.preventDefault()}
  }
},{passive:false});
function endPointer(e,cancel){
  if(edS&&e.pointerId===edS.id){const s=edS;const v=s.dx/Math.max(1,performance.now()-s.t0);
    if(!cancel&&(s.dx>s.w*.33||(v>.45&&s.dx>40))){pop()}
    else if(s.dx>2){s.top.el.classList.add('anim');s.prev.el.classList.add('anim');s.top.el.style.transform='translateX(0)';s.prev.el.style.transform='translateX(-30%)';setTimeout(()=>{s.top.el.classList.remove('anim');s.prev.el.classList.remove('anim');if(cur()===s.top)s.prev.el.style.visibility='hidden'},470)}
    else{s.top.el.style.transform='translateX(0)';s.prev.el.style.transform='translateX(-30%)';s.prev.el.style.visibility='hidden'}
    edS=null;noClick=Date.now()+300;return}
  if(swS&&e.pointerId===swS.id){const s=swS;swS=null;if(!s.on)return;noClick=Date.now()+350;s.row.classList.remove('drag');
    const W=s.row.offsetWidth,x=s.x,L=s.row.dataset.l,R=s.row.dataset.r;const nr=R?R.split(',').length:0;
    if(cancel){s.c.style.transform='';return}
    if(L&&x>W*.42){s.c.style.transform=`translateX(${W}px)`;setTimeout(()=>swAct(s.row,L.split(',')[0]),220)}
    else if(L&&x>56){s.c.style.transform='translateX(78px)';openSw=s.row}
    else if(R&&x<-W*.55){const ks=R.split(',');s.c.style.transform=`translateX(${-W}px)`;setTimeout(()=>swAct(s.row,ks[ks.length-1]),220)}
    else if(R&&x<-44){s.c.style.transform=`translateX(${-78*nr}px)`;openSw=s.row}
    else{s.c.style.transform='';setTimeout(()=>s.row.classList.remove('dl','dr'),350)}
    return}
  if(hS&&e.pointerId===hS.id){const dx=e.clientX-hS.x0,dy=e.clientY-hS.y0;const h=hS;hS=null;if(!cancel&&Math.abs(dx)>50&&Math.abs(dx)>Math.abs(dy)*1.5){noClick=Date.now()+350;const f=ACT[h.h.dataset.hsw];if(f)f(null,null,dx<0?1:-1)}}
}
device.addEventListener('pointerup',e=>endPointer(e,false));
device.addEventListener('pointercancel',e=>endPointer(e,true));
ACT.sw=t=>{const r=t.closest('.sw');openSw=null;swAct(r,t.dataset.k)};
function swAct(row,k){const ref=row.dataset.ref;const o=get(ref);if(!o)return;
  if(k==='done'){o.done=true;if(ref[0]==='t')o.progress=o.sittings.length?100:o.progress;hud('已完成');refreshAll()}
  else if(k==='later'){o.date=addD(o.date&&o.date>TODAY?o.date:TODAY,1);hud('已推迟到'+rel(o.date),'later');refreshAll()}
  else if(k==='del'){const arr=S[KIND[ref[0]]];arr.splice(arr.indexOf(o),1);hud('已删除','trash');refreshAll()}
  else if(k==='pin'){o.pinned=!o.pinned;hud(o.pinned?'已置顶':'已取消置顶','pinned');refreshAll()}
}
function swRow(ref,inner,L,R,extra){
  const lab={done:['check','完成'],later:['later','推迟'],del:['trash','删除'],pin:['pinned','置顶']};
  const b=k=>`<button class="k-${k}" data-act="sw" data-k="${k}">${ic(lab[k][0],22)}${lab[k][1]}</button>`;
  return `<div class="sw ${extra||''}" data-ref="${ref}" data-l="${L||''}" data-r="${R||''}">${L?`<div class="sw-a l">${L.split(',').map(b).join('')}</div>`:''}${R?`<div class="sw-a r">${R.split(',').map(b).join('')}</div>`:''}<div class="sw-c">${inner}</div></div>`;
}

/* keyboard offset (iOS) */
if(window.visualViewport){const vv=window.visualViewport;const upd=()=>{if(innerWidth>500){device.style.setProperty('--kb','0px');device.classList.remove('kb-open');return}const kb=Math.max(0,Math.round(innerHeight-vv.height-vv.offsetTop));device.style.setProperty('--kb',kb+'px');device.classList.toggle('kb-open',kb>80)};vv.addEventListener('resize',upd);vv.addEventListener('scroll',upd)}
function fit(){if(innerWidth>500){const s=Math.min(1,(innerHeight-40)/866);SCALE=s;device.style.transform=s<1?`scale(${s})`:'';device.style.margin=s<1?`${-(1-s)*422}px ${-(1-s)*195}px`:''}else{SCALE=1;device.style.transform='';device.style.margin=''}}
addEventListener('resize',fit);

/* ================= shared components ================= */
function ring(p,size,sw,color,from){const r=(size-sw)/2,C=2*Math.PI*r;const off=C*(1-Math.min(1,p));const fo=from!=null?C*(1-Math.min(1,from)):null;
  return `<svg class="ring" width="${size}" height="${size}" viewBox="0 0 ${size} ${size}"><circle cx="${size/2}" cy="${size/2}" r="${r}" stroke="var(--fill)" stroke-width="${sw}"/><circle class="fg" cx="${size/2}" cy="${size/2}" r="${r}" stroke="${color}" stroke-width="${sw}" stroke-linecap="round" stroke-dasharray="${C}" style="stroke-dashoffset:${fo!=null?fo:off}" ${fo!=null?`data-to="${off}"`:''}/></svg>`}
function spark(vals,w,h,color){const mn=Math.min(...vals),mx=Math.max(...vals),rg=mx-mn||1;const pts=vals.map((v,i)=>[(i/(vals.length-1))*(w-4)+2,h-2-((v-mn)/rg)*(h-4)]);
  return `<svg class="spark" width="${w}" height="${h}" viewBox="0 0 ${w} ${h}"><polyline points="${pts.map(p=>p.join(',')).join(' ')}" fill="none" stroke="${color}" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/><circle cx="${pts[pts.length-1][0]}" cy="${pts[pts.length-1][1]}" r="2.6" fill="${color}"/></svg>`}
function seg(items,val,act,extra){return `<div class="seg ${extra||''}">${items.map(([v,l])=>`<button class="${String(v)===String(val)?'on':''}" data-act="${act}" data-v="${v}">${l}</button>`).join('')}</div>`}
function spaceBtn(){const s=S.space;return `<button class="navbtn spbtn ${s!=='all'?'on':''}" data-act="space" aria-label="空间">${s==='all'?ic('filter',24):dot(s)+SPN[s]}</button>`}
ACT.space=t=>menu(t,[{label:'全部',checked:S.space==='all',fn:()=>{S.space='all';refreshAll()}},'-',{label:'个人',dot:'personal',checked:S.space==='personal',fn:()=>{S.space='personal';refreshAll()}},{label:'工作',dot:'work',checked:S.space==='work',fn:()=>{S.space='work';refreshAll()}}]);
const PRIO=['','!','!!','!!!'];
function goalBtn(g,anim){const p=gProg(g);const done=p.c>=p.t;
  if(g.dims&&g.dims.length)return `<button class="cbtn ${done?'done':''} ${anim?'pop':''}" data-act="gcheck" data-id="${g.id}" aria-label="打卡">${ic(done?'check':'plus',20)}</button>`;
  return `<button class="cbtn ${done?'done':''} ${anim?'pop':''}" data-act="gcheck" data-id="${g.id}" aria-label="打卡">${done?ic('check',20):(g.unit==='min'?'+'+Math.min(g.target,30):'+1')}</button>`}
function goalRow(g){const p=gProg(g);const a=S._anim&&S._anim.id===g.id?S._anim:null;const done=p.c>=p.t;
  const col=done?'var(--ok)':'var(--accent)';
  let sub=`<span class="${a?'bump':''}">${p.c}/${p.t}</span>${g.unit==='min'&&!g.dims.length?' 分钟':''} ${PER[g.period]}`;
  if(g.total)sub+=` · ${g.total.name} ${g.total.cur}/${g.total.max}`;
  if(g.dims.length)sub+=` · ${g.dims.length} 个维度`;
  const right=g.metric?`<div style="display:flex;flex-direction:column;align-items:flex-end;gap:1px">${spark(g.metric.readings.map(r=>r.v),48,22,'var(--ok)')}<span class="mono" style="font-size:11px;color:var(--label2)">${g.metric.name} ${g.metric.readings[g.metric.readings.length-1].v}</span></div>`:'';
  return `<div class="row tap tli" data-act="open" data-ref="g:${g.id}" style="padding-top:9px;padding-bottom:9px"><div class="grow-ring">${ring(p.c/p.t,40,4.5,col,a?a.from:null)}<b>${done?ic('check',14):''}</b></div><div class="grow"><div class="t1">${esc(g.title)}${g.streak?` <span class="streak" style="font-size:13px;font-weight:600;vertical-align:1px">${ic('flame',12)}${g.streak}${PERU[g.period]}</span>`:''}</div><div class="t2">${sub}</div></div>${right}${goalBtn(g,!!a)}</div>`}
function projCard(p){const s=pStats(p);const pc=s.total?s.done/s.total:0;
  return `<button class="pcard" data-act="open" data-ref="p:${p.id}"><div class="h">${dot(p.space)}<span>${esc(p.title)}</span>${p.archived?'<small class="muted" style="font-size:12px;font-weight:500">已归档</small>':''}</div><div class="bar"><i style="width:${pc*100}%;background:var(--${p.space})"></i></div><div class="m num">${s.done}/${s.total} 待办 · ${s.left>=0?'还剩 '+s.left+' 天':'已过期 '+(-s.left)+' 天'}</div>${s.next?`<div class="n">${ic('chev',12)}<span>${esc(s.next.title)}${s.next.date?' · '+rel(s.next.date):''}</span></div>`:'<div class="n muted">没有待办</div>'}</button>`}
