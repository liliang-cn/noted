
/* ================= helpers for lists ================= */
const grp=(h,cls)=>`<div class="sec"><div class="group ${cls||''}">${h}</div></div>`;
const grpH=(head,h,cls,foot)=>`<div class="sec"><div class="sh">${head}</div><div class="group ${cls||''}">${h}</div>${foot?`<div class="sf">${foot}</div>`:''}</div>`;
const OPT={
 remind:[['none','无'],['at','准时'],['5','提前 5 分钟'],['15','提前 15 分钟'],['30','提前 30 分钟'],['60','提前 1 小时'],['1440','提前 1 天']],
 repeat:[['none','永不'],['daily','每天'],['weekly','每周'],['monthly','每月'],['yearly','每年']],
 prio:[['0','无'],['1','低'],['2','中'],['3','高']],
};
const optL=(f,v)=>{const x=OPT[f].find(o=>String(o[0])===String(v));return x?x[1]:''};
const WORDER=[1,2,3,4,5,6,0];
function repL(o){if(o.repeat==='weekly'&&o.days&&o.days.length)return '每周'+WORDER.filter(d=>o.days.includes(d)).map(d=>WD[d]).join('、');return optL('repeat',o.repeat)}

function evTL(e,nm){const past=e.date===TODAY&&!e.allDay&&hm(e.end)<=nm;const now=e.date===TODAY&&!e.allDay&&hm(e.start)<=nm&&hm(e.end)>nm;
  const p=e.project?proj(e.project):null;
  const sub=[now?'<b style="color:var(--danger);font-weight:600">进行中</b>':'',esc(e.location),e.allDay?'':dur(hm(e.end)-hm(e.start)),p?ic('folder',12)+esc(p.title):''].filter(Boolean).join(' · ');
  return `<button class="tl tap tli ${past?'past':''}" data-act="open" data-ref="e:${e.id}"><div class="tl-time">${e.allDay?'全天':`${e.start}<span>${e.end}</span>`}</div><div class="tl-bar" style="background:var(--${e.space})"></div><div class="grow"><div class="t1">${esc(e.title)}</div>${sub?`<div class="t2">${sub}</div>`:''}</div></button>`}
function todoTL(t,nm,showDate){const od=!t.done&&t.date&&(t.date<TODAY||(t.date===TODAY&&t.time&&hm(t.time)<nm));
  const parts=[];if(od)parts.push(`<span class="red">${t.date<TODAY?rel(t.date)+' · ':''}已逾期</span>`);else if(showDate&&t.date)parts.push(rel(t.date));
  const p=t.project?proj(t.project):null;if(p)parts.push(ic('folder',12)+esc(p.title));
  if(t.sittings.length||t.progress)parts.push(`<span class="num">${t.progress}%</span>`);
  if(t.repeat!=='none')parts.push(ic('repeat',12)+repL(t));
  if(t.remind!=='none'&&!t.done)parts.push(ic('bell',12));
  const inner=`<div class="tl tap ${t.done?'done':''}" data-act="open" data-ref="t:${t.id}"><div class="tl-time ${od?'red':''}">${t.time||''}</div><button class="ckb" data-act="tdone" data-id="${t.id}" aria-label="完成"><span class="ck ${t.done?'on':''} ${S._animT===t.id?'pop':''}">${t.done?ic('check',14):''}</span></button><div class="grow"><div class="t1">${t.prio?`<span class="red" style="font-weight:700">${PRIO[t.prio]} </span>`:''}${esc(t.title)}</div>${parts.length?`<div class="t2">${parts.join(' · ')}</div>`:''}</div>${dot(t.space)}</div>`;
  return swRow('t:'+t.id,inner,t.done?'':'done','later,del','tli');
}

/* ================= 今天 ================= */
SC.today={title:'今天',large:true,sub:()=>mdw(TODAY),
 right:()=>spaceBtn()+`<button class="navbtn" data-act="push" data-to="settings" aria-label="设置"><span class="av">LL</span></button>`,
 body(){
  const nm=nowMin();
  const evs=S.events.filter(e=>e.date===TODAY&&spOK(e));
  const tds=S.todos.filter(t=>t.date===TODAY&&spOK(t));
  const od=S.todos.filter(t=>!t.done&&t.date&&t.date<TODAY&&spOK(t));
  const timed=[];
  evs.filter(e=>!e.allDay).forEach(e=>timed.push({m:hm(e.start),h:evTL(e,nm)}));
  tds.filter(t=>t.time).forEach(t=>timed.push({m:hm(t.time),h:todoTL(t,nm)}));
  timed.sort((a,b)=>a.m-b.m);
  const rows=od.map(t=>todoTL(t,nm));
  const nowRow=`<div class="nowl nosep"><span class="num">${nowHM()}</span><i></i></div>`;
  let placed=false;timed.forEach(x=>{if(!placed&&x.m>nm){rows.push(nowRow);placed=true}rows.push(x.h)});
  if(!placed&&timed.length)rows.push(nowRow);
  const un=[...evs.filter(e=>e.allDay).map(e=>evTL(e,nm)),...tds.filter(t=>!t.time).sort((a,b)=>a.done-b.done).map(t=>todoTL(t,nm))];
  if(un.length){rows.push('<div class="tl-lab nosep">全天 · 未定时</div>');rows.push(...un)}
  let h=`<div class="sec"><div class="sh2"><h2>日程与待办</h2><button class="more" data-act="tab" data-tab="cal">日历${ic('chev',14)}</button></div><div class="group">${rows.join('')||'<div class="empty">今天没有安排</div>'}</div></div>`;
  const prog=S.todos.filter(t=>!t.done&&t.sittings.length&&spOK(t));
  if(prog.length)h+=`<div class="sec"><div class="sh2"><h2>进行中</h2></div><div class="group">${prog.map(t=>{const tot=t.sittings.reduce((a,s)=>a+s.min,0);const last=t.sittings[t.sittings.length-1];
    return `<button class="prow tap" data-act="open" data-ref="t:${t.id}"><div class="top">${dot(t.space)}<span class="t1">${esc(t.title)}</span><span class="pc mono">${t.progress}%</span></div><div class="bar"><i style="width:${t.progress}%"></i></div><div class="t2">已做 ${t.sittings.length} 次 · 共 ${dur(tot)} · 上次 ${rel(last.date)}</div></button>`}).join('')}</div></div>`;
  const gs=S.goals.filter(spOK);
  if(gs.length)h+=`<div class="sec"><div class="sh2"><h2>今天打卡</h2><button class="more" data-act="push" data-to="goals">全部${ic('chev',14)}</button></div><div class="group">${gs.map(goalRow).join('')}</div></div>`;
  const ps=S.projects.filter(p=>p.pinned&&!p.archived&&spOK(p));
  if(ps.length)h+=`<div class="sec" style="margin-left:0;margin-right:0"><div class="sh2" style="padding:0 20px 8px"><h2>置顶项目</h2><button class="more" data-act="push" data-to="projects">全部${ic('chev',14)}</button></div><div class="pcards" data-hx="pc">${ps.map(projCard).join('')}</div></div>`;
  return h;
 }};

/* ================= 日历 ================= */
const HH=52;
function layoutCols(items){items.sort((a,b)=>a.s-b.s||b.e-a.e);let cols=[],group=[],gEnd=-1;
  const flush=()=>{const n=cols.length;group.forEach(x=>x.n=n);group=[];cols=[]};
  items.forEach(it=>{if(group.length&&it.s>=gEnd)flush();let c=cols.findIndex(end=>end<=it.s);if(c<0){c=cols.length;cols.push(it.e)}else cols[c]=it.e;it.col=c;group.push(it);gEnd=Math.max(gEnd,it.e)});
  if(group.length)flush();return items}
function dayItems(date){const nm=nowMin();const it=[];
  S.events.filter(e=>e.date===date&&!e.allDay&&spOK(e)).forEach(e=>it.push({s:hm(e.start),e:Math.max(hm(e.end),hm(e.start)+20),ev:e,past:date<TODAY||(date===TODAY&&hm(e.end)<=nm)}));
  S.todos.filter(t=>t.date===date&&t.time&&spOK(t)).forEach(t=>it.push({s:hm(t.time),e:hm(t.time)+30,td:t}));
  return layoutCols(it)}
function evBlock(x,H,L,Wc){const top=x.s/60*H+8,ht=Math.max(22,(x.e-x.s)/60*H-2);
  const pos=`top:${top}px;height:${ht}px;left:calc(${L} + ${Wc} * ${x.col/x.n} + 1px);width:calc(${Wc} / ${x.n} - 3px)`;
  if(x.td){const t=x.td;return `<button class="evb todo" style="--c:var(--${t.space});${pos};height:22px" data-act="open" data-ref="t:${t.id}"><span class="ck sm ${t.done?'on':''}" style="width:12px;height:12px;border-width:1.3px"></span><b style="${t.done?'text-decoration:line-through;opacity:.6':''}">${esc(t.title)}</b></button>`}
  const e=x.ev;return `<button class="evb ${x.past?'past':''}" style="--c:var(--${e.space});${pos}" data-act="open" data-ref="e:${e.id}"><b>${esc(e.title)}</b>${ht>38?`<span>${e.start}${e.location?' · '+esc(e.location):''}</span>`:''}</button>`}
function nowLine(H){const nm=nowMin();const y=nm/60*H+8;return `<div class="nowline" style="top:${y}px"></div><div class="nowlab num" style="top:${y-7}px">${nowHM()}</div>`}
function dayGrid(date){let h=`<div class="grid" data-act="calslot" data-date="${date}" style="height:${24*HH+16}px">`;
  for(let i=0;i<24;i++)h+=`<div class="hl" style="top:${i*HH+8}px">${i?`<span>${pad(i)}:00</span>`:''}</div>`;
  h+=dayItems(date).map(x=>evBlock(x,HH,'54px','(100% - 62px)')).join('');
  if(date===TODAY)h+=nowLine(HH);
  return h+'</div>'}
function weekGrid(m){const H=46;let h=`<div class="grid wkgrid" data-act="calslot" data-week="${m}" style="height:${24*H+16}px">`;
  for(let i=0;i<24;i++)h+=`<div class="hl" style="top:${i*H+8}px;margin-left:44px">${i?`<span style="left:-42px;width:36px">${pad(i)}</span>`:''}</div>`;
  for(let d=1;d<7;d++)h+=`<div style="position:absolute;top:0;bottom:0;left:calc(44px + (100% - 46px) * ${d/7});border-left:.5px solid var(--sep)"></div>`;
  for(let d=0;d<7;d++){const ds=addD(m,d);const L=`(44px + (100% - 46px) * ${d/7})`;
    h+=dayItems(ds).map(x=>evBlock(x,H,L,'((100% - 46px) / 7)').replace('<span>','<span style="display:none">')).join('')}
  if(TODAY>=m&&TODAY<=addD(m,6)){const y=nowMin()/60*H+8;h+=`<div class="nowline" style="top:${y}px;left:44px"></div>`}
  return h+'</div>'}
function dayDots(d){const e=S.events.filter(x=>x.date===d&&spOK(x));const t=S.todos.filter(x=>x.date===d&&!x.done&&spOK(x));return{e,t}}
SC.cal={title:'日历',cls:'cal',navcls:'solid flat',white:()=>S.cal.mode!=='month',
 left:()=>`<button class="navbtn" data-act="caltoday">今天</button>`,
 center:()=>`<div style="width:150px">${seg([['day','日'],['week','周'],['month','月']],S.cal.mode,'calmode','sm')}</div>`,
 right:()=>spaceBtn(),
 mount(p,scr){if(!scr.st.scrolled&&scr.el.offsetParent!==null&&tvs.cal.classList.contains('on')){const sc=$('.scroll',scr.el);sc.scrollTop=S.cal.mode==='month'?0:(S.cal.mode==='day'?7.5*HH:7.5*46);scr.st.scrolled=true}},
 body(){const c=S.cal;const sel=c.sel;const d=s2d(c.mode==='month'?c.mon:sel);
  const head=`<div class="cal-head"><div class="t"><b>${d.getMonth()+1}月</b><span>${d.getFullYear()}</span></div><button class="navbtn" data-act="calnav" data-d="-1" aria-label="上一个">${ic('chevl',22)}</button><button class="navbtn" data-act="calnav" data-d="1" aria-label="下一个">${ic('chev',22)}</button></div>`;
  if(c.mode==='day'){const m=monday(sel);
    const strip=`<div class="wk" data-hsw="calnav">${[0,1,2,3,4,5,6].map(i=>{const ds=addD(m,i);const dd=dayDots(ds);const n=Math.min(3,dd.e.length+dd.t.length);
      return `<button class="${ds===sel?'sel':''} ${ds===TODAY?'today':''}" data-act="calsel" data-v="${ds}"><span class="w">${WD[s2d(ds).getDay()]}</span><span class="d">${s2d(ds).getDate()}</span><span class="dd">${'<i></i>'.repeat(n)}</span></button>`}).join('')}</div>`;
    const ad=[...S.events.filter(e=>e.date===sel&&e.allDay&&spOK(e)).map(e=>`<button class="ad" style="background:color-mix(in srgb,var(--${e.space}) 13%,#fff);color:var(--${e.space})" data-act="open" data-ref="e:${e.id}">${esc(e.title)}</button>`),
      ...S.todos.filter(t=>t.date===sel&&!t.time&&spOK(t)).map(t=>`<button class="ad" style="background:var(--fill2)" data-act="open" data-ref="t:${t.id}"><span class="ck sm ${t.done?'on':''}" style="width:12px;height:12px;border-width:1.3px"></span><span style="${t.done?'text-decoration:line-through;color:var(--label2)':''}">${esc(t.title)}</span></button>`)];
    return `<div class="calpad"></div><div class="cal-sticky">${head}${strip}${ad.length?`<div class="alld"><span class="lab">全天</span>${ad.join('')}</div>`:''}</div><div class="sh" style="padding:10px 16px 0">${mdw(sel)}${sel===TODAY?' · 今天':''}</div>${dayGrid(sel)}`}
  if(c.mode==='week'){const m=monday(sel);
    const hd=`<div class="wkhead" data-hsw="calnav"><span></span>${[0,1,2,3,4,5,6].map(i=>{const ds=addD(m,i);return `<button class="${ds===TODAY?'today':''}" data-act="calday" data-v="${ds}">${WD[s2d(ds).getDay()]}<b>${s2d(ds).getDate()}</b></button>`}).join('')}</div>`;
    return `<div class="calpad"></div><div class="cal-sticky">${head}${hd}</div>${weekGrid(m)}`}
  const m0=c.mon;const st=monday(m0);const d0=s2d(m0);const dim=new Date(d0.getFullYear(),d0.getMonth()+1,0).getDate();const rowsN=Math.ceil((diffD(st,m0)+dim)/7);
  let g=`<div class="mgrid" data-hsw="calnav">${WORDER.map(x=>`<span class="wh">${WD[x]}</span>`).join('')}`;
  for(let i=0;i<rowsN*7;i++){const ds=addD(st,i);const dd=dayDots(ds);const dots=[...dd.e.slice(0,3).map(e=>`<i style="background:var(--${e.space})"></i>`),...(dd.e.length<3&&dd.t.length?['<i style="background:var(--label3)"></i>']:[])].join('');
    g+=`<button class="${ds.slice(0,7)!==m0.slice(0,7)?'out':''} ${ds===TODAY?'today':''} ${ds===sel?'sel':''}" data-act="calsel" data-v="${ds}"><span class="d">${s2d(ds).getDate()}</span><span class="dd">${dots}</span></button>`}
  g+='</div>';
  const nm=nowMin();const evs=S.events.filter(e=>e.date===sel&&spOK(e)).sort((a,b)=>(a.allDay?-1:hm(a.start))-(b.allDay?-1:hm(b.start)));const tds=S.todos.filter(t=>t.date===sel&&spOK(t));
  const list=[...evs.map(e=>({k:e.allDay?-1:hm(e.start),h:evTL(e,nm)})),...tds.map(t=>({k:t.time?hm(t.time):2000,h:todoTL(t,nm)}))].sort((a,b)=>a.k-b.k).map(x=>x.h).join('');
  return `<div class="calpad"></div><div class="cal-sticky">${head}</div>${g}<div style="padding-top:16px">${grpH(mdw(sel),list||'<div class="empty">没有安排</div>')}</div>`;
 }};
ACT.calnav=(t,e,dir)=>{dir=dir||+t.dataset.d;const c=S.cal;if(c.mode==='month'){c.mon=addM(c.mon,dir);if(c.sel.slice(0,7)!==c.mon.slice(0,7))c.sel=c.mon===TODAY.slice(0,7)+'-01'?TODAY:c.mon}else{c.sel=addD(c.sel,7*dir);c.mon=c.sel.slice(0,7)+'-01'}render(cur())};
ACT.caltoday=()=>{const c=S.cal;c.sel=TODAY;c.mon=TODAY.slice(0,7)+'-01';const s=cur();s.st.scrolled=false;render(s)};
ACT.calmode=t=>{S.cal.mode=t.dataset.v;if(S.cal.mode==='month')S.cal.mon=S.cal.sel.slice(0,7)+'-01';const s=cur();s.st.scrolled=false;render(s)};
ACT.calsel=t=>{S.cal.sel=t.dataset.v;S.cal.mon=t.dataset.v.slice(0,7)+'-01';render(cur())};
ACT.calday=t=>{S.cal.sel=t.dataset.v;S.cal.mode='day';const s=cur();s.st.scrolled=false;render(s)};
ACT.calslot=(t,e)=>{if(e.target!==t&&!e.target.classList.contains('hl'))return;const r=t.getBoundingClientRect();const y=(e.clientY-r.top)/SCALE-8;
  if(t.dataset.week){const H=46;const x=(e.clientX-r.left)/SCALE;const W=r.width/SCALE;const col=Math.max(0,Math.min(6,Math.floor((x-44)/((W-46)/7))));const hr=Math.max(0,Math.min(23,Math.floor(y/H)));push('new',{kind:'event',date:addD(t.dataset.week,col),start:pad(hr)+':00'});return}
  const hr=Math.max(0,Math.min(23,Math.floor(y/HH)));push('new',{kind:'event',date:t.dataset.date,start:pad(hr)+':00'})};

/* ================= 计划 ================= */
function holdRow(h){const pl=(h.last-h.avg)*h.shares,pp=(h.last/h.avg-1)*100;
  return `<button class="row" data-act="open" data-ref="h:${h.id}" style="padding-top:10px;padding-bottom:10px"><div class="grow"><div class="t1" style="font-weight:600">${h.sym}</div><div class="t2 num">${h.shares} 股 · 均价 ${h.avg.toFixed(2)}</div></div><div style="text-align:right"><div class="mono" style="font-size:16px">${h.last.toFixed(2)}</div><div class="mono ${pl>=0?'green':'red'}" style="font-size:12px">${money(pl,true)}</div></div><span class="plpill mono" style="background:${pl>=0?'var(--ok)':'var(--danger)'}">${pctf(pp,true)}</span></button>`}
function nextDcaAll(){const on=S.holdings.filter(h=>h.dca.on&&h.shares>=0);if(!on.length)return null;const ds=on.map(nextDca).sort();const d=ds[0];return{d,amt:on.filter(h=>nextDca(h)===d).reduce((a,h)=>a+h.dca.amount,0)}}
function invSummary(tap){const t=holdTot();const n=nextDcaAll();
  return `<${tap?'button':'div'} class="card ${tap?'tap':''}" ${tap?'data-act="push" data-to="invest"':''} style="display:block;width:100%;text-align:left"><div class="muted" style="font-size:13px">总市值 · USD</div><div class="inv-total mono">${money(t.v)}</div><div class="mono ${t.pl>=0?'green':'red'}" style="font-size:15px">${money(t.pl,true)}　${pctf(t.pp,true)}</div><div class="kv" style="border-top:.5px solid var(--sep);margin-top:12px;padding-top:11px"><span>下次定投</span><span class="num">${n?`${mdw(n.d)} · ${money(n.amt).replace('.00','')}`:'已暂停'}</span></div></${tap?'button':'div'}>`}
SC.plan={title:'计划',large:true,right:()=>spaceBtn(),
 body(){const ps=S.projects.filter(p=>!p.archived&&spOK(p));const gs=S.goals.filter(spOK);
  let h=`<div class="sec"><div class="sh2"><h2>项目</h2><button class="more" data-act="push" data-to="projects">全部${ic('chev',14)}</button></div><div class="cards plist">${ps.map(projCard).join('')||'<div class="card empty">没有进行中的项目</div>'}</div></div>`;
  h+=`<div class="sec"><div class="sh2"><h2>目标</h2><button class="more" data-act="push" data-to="goals">全部${ic('chev',14)}</button></div><div class="group">${gs.map(goalRow).join('')||'<div class="empty">没有目标</div>'}</div></div>`;
  if(S.space!=='work')h+=`<div class="sec"><div class="sh2"><h2>投资</h2><button class="more" data-act="push" data-to="invest">全部${ic('chev',14)}</button></div>${invSummary(true)}<div class="group" style="margin-top:10px">${S.holdings.map(holdRow).join('')}</div></div>`;
  return h}};

/* ================= 笔记 ================= */
function noteRow(n){const p=n.project?proj(n.project):null;
  const inner=`<div class="nrow tap" data-act="open" data-ref="n:${n.id}"><div class="t1">${esc(n.title||'无标题')}</div><div class="pv">${esc(stripMd(n.body))||'无内容'}</div><div class="mt"><span class="num">${relTime(n.updated)}</span>${dot(n.space)}${p?`<span style="display:inline-flex;gap:3px;align-items:center">${ic('folder',12)}${esc(p.title)}</span>`:''}${n.tags.map(t=>`<span class="tg">#${esc(t)}</span>`).join('')}</div></div>`;
  return swRow('n:'+n.id,inner,'pin','del')}
function notesList(){let ns=S.notes.filter(spOK);if(S.noteTag!=='全部')ns=ns.filter(n=>n.tags.includes(S.noteTag));
  const q=S.noteQ.trim().toLowerCase();if(q)ns=ns.filter(n=>(n.title+' '+n.body+' '+n.tags.join(' ')).toLowerCase().includes(q));
  ns.sort((a,b)=>b.updated.localeCompare(a.updated));
  if(!ns.length)return `<div class="empty">${q?'没有找到“'+esc(S.noteQ)+'”':'没有笔记'}</div>`;
  const pin=ns.filter(n=>n.pinned),rest=ns.filter(n=>!n.pinned);const wk=addD(TODAY,-6);
  const buckets=[['今天',n=>n.updated.slice(0,10)===TODAY],['过去 7 天',n=>n.updated.slice(0,10)<TODAY&&n.updated.slice(0,10)>=wk],['更早',n=>n.updated.slice(0,10)<wk]];
  let h=pin.length?grpH('<span>'+ic('pinned',13).replace('class="ic ','style="display:inline-block;vertical-align:-2px" class="ic ')+' 置顶</span>',pin.map(noteRow).join('')):'';
  buckets.forEach(([l,f])=>{const xs=rest.filter(f);if(xs.length)h+=grpH(l,xs.map(noteRow).join(''))});return h}
SC.notes={title:'笔记',large:true,
 right:()=>spaceBtn()+`<button class="navbtn" data-act="newnote" aria-label="新笔记">${ic('compose',24)}</button>`,
 body(){const tags=['全部',...new Set(S.notes.flatMap(n=>n.tags))];
  return `<div class="search">${ic('search',18)}<input type="search" placeholder="搜索" value="${esc(S.noteQ)}" data-in="noteq" enterkeyhint="search" autocomplete="off"></div><div class="hx" data-hx="tags">${tags.map(t=>`<button class="chip ${S.noteTag===t?'on':''}" data-act="notetag" data-v="${esc(t)}">${t==='全部'?t:'#'+esc(t)}</button>`).join('')}</div><div class="nlist">${notesList()}</div>`}};
IN.noteq=t=>{S.noteQ=t.value;const l=$('.nlist',cur().el);if(l)l.innerHTML=notesList()};
ACT.notetag=t=>{S.noteTag=t.dataset.v;render(cur())};
ACT.newnote=()=>push('new',{kind:'note'});

/* ================= form rows ================= */
function pickRow(f,label,icn,color,val){return `<button class="row ir" data-act="pick" data-f="${f}">${tile(icn,color)}<span class="grow">${label}</span><span class="val">${val}</span>${ic('updown',16,'chev')}</button>`}
function swRowF(f,label,icn,color,on,sub){return `<div class="row ir">${tile(icn,color)}<button class="rl" data-act="${on?'exp':'tgl'}" data-f="${f}" style="text-align:left;align-self:stretch;justify-content:center">${label}${on&&sub?`<small>${sub}</small>`:''}</button><button class="switch ${on?'on':''}" data-act="tgl" data-f="${f}" aria-label="${label}"></button></div>`}
function segF(f,items,val,w){return `<div class="seg sm" style="width:${w||132}px">${items.map(([v,l])=>`<button class="${String(val)===String(v)?'on':''}" data-act="setf" data-f="${f}" data-v="${v}">${l}</button>`).join('')}</div>`}
const spaceRow=o=>`<div class="row ir">${tile('person','graphite')}<span class="grow">空间</span>${segF('space',[['personal','个人'],['work','工作']],o.space)}</div>`;
const projRow=o=>pickRow('project','项目','folder','teal',o.project&&proj(o.project)?esc(proj(o.project).title):'无');
const daysRow=o=>`<div class="days">${WORDER.map(d=>`<button class="${o.days.includes(d)?'on':''}" data-act="day" data-v="${d}">${WD[d]}</button>`).join('')}</div>`;
const noteTA=o=>`<textarea class="ta ag" data-in="f" data-f="note" placeholder="备注" rows="2">${esc(o.note)}</textarea>`;
function miniCal(sel,f,scr){const m=scr.st.mcm||(sel||TODAY).slice(0,7)+'-01';const d0=s2d(m);const off=(d0.getDay()+6)%7;const dim=new Date(d0.getFullYear(),d0.getMonth()+1,0).getDate();
  let h=`<div class="mc"><div class="mc-h"><b>${d0.getFullYear()}年${d0.getMonth()+1}月</b><button class="navbtn" data-act="mcnav" data-d="-1" aria-label="上个月">${ic('chevl',20)}</button><button class="navbtn" data-act="mcnav" data-d="1" aria-label="下个月">${ic('chev',20)}</button></div><div class="mc-g">${WORDER.map(x=>`<span class="wh">${WD[x]}</span>`).join('')}`;
  for(let i=0;i<off;i++)h+='<button class="out" disabled></button>';
  for(let i=1;i<=dim;i++){const s=m.slice(0,8)+pad(i);h+=`<button class="${s===sel?'sel':''} ${s===TODAY?'today':''}" data-act="mcpick" data-f="${f}" data-v="${s}"><span>${i}</span></button>`}
  return h+'</div></div>'}
function wheel(f,val){const[h,m]=(val||'09:00').split(':').map(Number);const mi=Math.round(m/5)%12;
  return `<div class="wheel" data-f="${f}"><div class="band"></div><div class="wcol" data-p="h" data-i="${h}">${Array.from({length:24},(_,i)=>`<button data-act="wpick" data-i="${i}">${pad(i)}</button>`).join('')}</div><div class="wsep">:</div><div class="wcol" data-p="m" data-i="${mi}">${Array.from({length:12},(_,i)=>`<button data-act="wpick" data-i="${i}">${pad(i*5)}</button>`).join('')}</div></div>`}
function wheelScroll(col){if(col._prog)return;clearTimeout(col._t);col._t=setTimeout(()=>{const i=Math.max(0,Math.round(col.scrollTop/36));col.dataset.i=i;const w=col.closest('.wheel');if(!w)return;const o=tgt();if(!o)return;const cs=$$('.wcol',w);setTime(o,w.dataset.f,pad(+cs[0].dataset.i)+':'+pad(+cs[1].dataset.i*5))},110)}
function setTime(o,f,v){if(o[f]===v)return;if(f==='start'&&o.end){const d=hm(o.end)-hm(o.start);o.start=v;o.end=mhm(Math.min(hm(v)+Math.max(d,15),1435))}else o[f]=v;
  $$('[data-val]',cur().el).forEach(x=>{const k=x.dataset.val;if(o[k]!=null)x.textContent=o[k]});if(cur().kind!=='new')soft()}
ACT.wpick=t=>{t.parentNode.scrollTo({top:+t.dataset.i*36,behavior:RM?'auto':'smooth'})};
ACT.mcnav=t=>{const s=cur();const o=tgt();const f=s.st.exp;s.st.mcm=addM(s.st.mcm||((o&&o[f])||TODAY).slice(0,7)+'-01',+t.dataset.d);render(s)};
ACT.mcpick=t=>{const o=tgt(),s=cur();o[t.dataset.f]=t.dataset.v;s.st.exp=null;s.st.mcm=null;render(s);if(s.kind!=='new')soft()};
ACT.exp=t=>{const s=cur();const f=t.dataset.f;s.st.exp=s.st.exp===f?null:f;s.st.mcm=null;render(s)};
ACT.tgl=t=>{const o=tgt(),f=t.dataset.f,s=cur();
  if(f==='date'){if(o.date){o.date=null;o.time=null;s.st.exp=null}else{o.date=TODAY;s.st.exp='date'}}
  else if(f==='time'){if(o.time){o.time=null;s.st.exp=null}else{o.time=o.date===TODAY?mhm(Math.min(Math.ceil((nowMin()+1)/60)*60,1380)):'09:00';s.st.exp='time'}}
  else{o[f]=!o[f];if(f==='allDay')s.st.exp=null}
  render(s);if(s.kind!=='new')soft()};
function setF(o,f,v){o[f]=v;if(f==='repeat'&&v==='weekly'&&!(o.days&&o.days.length))o.days=[s2d(o.date||TODAY).getDay()];
  if(f==='project'&&v){const p=proj(v);if(p)o.space=p.space}
  if(f==='unit'){if(v==='min'&&o.target<5)o.target=30;if(v==='times'&&o.target>20)o.target=3}
  const s=cur();render(s);if(s.kind!=='new')soft()}
ACT.setf=t=>setF(tgt(),t.dataset.f,t.dataset.v);
ACT.day=t=>{const o=tgt();const d=+t.dataset.v;o.days=o.days.includes(d)?o.days.filter(x=>x!==d):[...o.days,d];if(!o.days.length)o.days=[d];render(cur());soft()};
ACT.step=t=>{const o=tgt();const f=t.dataset.f;const st=f==='target'&&o.unit==='min'?5:(f==='totalMax'?5:1);o[f]=Math.max(1,(+o[f]||0)+st*(+t.dataset.d));render(cur());if(cur().kind!=='new')soft()};
ACT.pick=t=>{const o=tgt(),f=t.dataset.f;let items;
  if(f==='project')items=[{label:'无',checked:!o.project,fn:()=>setF(o,'project',null)},'-',...S.projects.filter(p=>!p.archived||p.id===o.project).map(p=>({label:p.title,dot:p.space,checked:o.project===p.id,fn:()=>setF(o,'project',p.id)}))];
  else if(f==='space')items=[['personal','个人'],['work','工作']].map(([v,l])=>({label:l,dot:v,checked:o.space===v,fn:()=>setF(o,'space',v)}));
  else items=OPT[f].map(([v,l])=>({label:l,checked:String(o[f])===String(v),fn:()=>setF(o,f,f==='prio'?+v:v)}));
  menu(t,items)};
function todoForm(o,s){const ex=s.st.exp;
  let g1=swRowF('date','日期','cal','red',!!o.date,o.date?mdw(o.date):'')+(ex==='date'&&o.date?`<div class="inl">${miniCal(o.date,'date',s)}</div>`:'');
  if(o.date)g1+=swRowF('time','时间','clock','blue',!!o.time,o.time?`<span data-val="time">${o.time}</span>`:'')+(ex==='time'&&o.time?`<div class="inl">${wheel('time',o.time)}</div>`:'');
  if(o.date)g1+=pickRow('remind','提醒','bell','orange',optL('remind',o.remind));
  g1+=pickRow('repeat','重复','repeat','gray',optL('repeat',o.repeat))+(o.repeat==='weekly'?daysRow(o):'');
  const g2=projRow(o)+spaceRow(o)+pickRow('prio','优先级','flag','rust',optL('prio',o.prio));
  return grp(g1)+grp(g2)+grp(noteTA(o))}
function eventForm(o,s){const ex=s.st.exp;
  let g1=`<div class="row ir">${tile('sun','orange')}<span class="grow">全天</span><button class="switch ${o.allDay?'on':''}" data-act="tgl" data-f="allDay" aria-label="全天"></button></div>`;
  g1+=`<div class="row ir">${tile('cal','red')}<span class="grow">${o.allDay?'日期':'开始'}</span><span class="rv"><button class="pill ${ex==='date'?'acc':''}" data-act="exp" data-f="date">${mdw(o.date)}</button>${o.allDay?'':`<button class="pill num ${ex==='start'?'acc':''}" data-act="exp" data-f="start" data-val="start">${o.start}</button>`}</span></div>`;
  if(ex==='date')g1+=`<div class="inl">${miniCal(o.date,'date',s)}</div>`;
  if(ex==='start'&&!o.allDay)g1+=`<div class="inl">${wheel('start',o.start)}</div>`;
  if(!o.allDay){g1+=`<div class="row ir">${tile('clock','blue')}<span class="grow">结束</span><span class="rv"><button class="pill num ${ex==='end'?'acc':''}" data-act="exp" data-f="end" data-val="end">${o.end}</button></span></div>`;if(ex==='end')g1+=`<div class="inl">${wheel('end',o.end)}</div>`}
  const g2=`<div class="row ir">${tile('pin','green')}<input class="ri l" data-in="f" data-f="location" placeholder="地点" value="${esc(o.location)}" autocomplete="off"></div>`;
  const g3=pickRow('remind','提醒','bell','orange',optL('remind',o.remind))+pickRow('repeat','重复','repeat','gray',optL('repeat',o.repeat))+(o.repeat==='weekly'?daysRow(o):'');
  return grp(g1)+grp(g2)+grp(g3)+grp(projRow(o)+spaceRow(o))+grp(noteTA(o))}
function projForm(o,s,isNew){const ex=s.st.exp;const sk=isNew?'pStart':'start',dk=isNew?'pDue':'due';
  let g1=`<div class="row ir">${tile('cal','red')}<span class="grow">开始</span><button class="pill ${ex===sk?'acc':''}" data-act="exp" data-f="${sk}">${mdw(o[sk])}</button></div>`+(ex===sk?`<div class="inl">${miniCal(o[sk],sk,s)}</div>`:'');
  g1+=`<div class="row ir">${tile('flag','rust')}<span class="grow">截止</span><span class="muted num" style="font-size:15px">${diffD(TODAY,o[dk])>=0?diffD(TODAY,o[dk])+' 天后':''}</span><button class="pill ${ex===dk?'acc':''}" data-act="exp" data-f="${dk}">${mdw(o[dk])}</button></div>`+(ex===dk?`<div class="inl">${miniCal(o[dk],dk,s)}</div>`:'');
  const g2=spaceRow(o)+`<div class="row ir">${tile('pinned','orange')}<span class="grow">置顶</span><button class="switch ${o.pinned?'on':''}" data-act="tgl" data-f="pinned" aria-label="置顶"></button></div>`;
  return grp(g1)+grp(g2)+grp(noteTA(o))}
function goalForm(o,s,isNew){
  let g1=`<div class="row ir">${tile('repeat','gray')}<span class="grow">周期</span>${segF('period',[['day','每天'],['week','每周'],['month','每月']],o.period,168)}</div>`;
  g1+=`<div class="row ir">${tile('target','blue')}<span class="grow">${PERN[o.period]} <b class="num">${o.target}</b> ${o.unit==='min'?'分钟':'次'}</span><div class="stepper"><button data-act="step" data-f="target" data-d="-1" aria-label="减少">${ic('minus',18)}</button><button data-act="step" data-f="target" data-d="1" aria-label="增加">${ic('plus',18)}</button></div></div>`;
  g1+=`<div class="row ir">${tile('clock','orange')}<span class="grow">计量</span>${segF('unit',[['times','次数'],['min','分钟']],o.unit)}</div>`;
  let h=grp(g1);
  if(isNew){
    let g2=`<div class="row ir">${tile('chart','teal')}<span class="grow">累计总量</span><button class="switch ${o.hasTotal?'on':''}" data-act="tgl" data-f="hasTotal" aria-label="累计总量"></button></div>`;
    if(o.hasTotal)g2+=`<div class="row ir"><span style="width:30px"></span><span class="grow">名称</span><input class="ri" data-in="f" data-f="totalName" value="${esc(o.totalName)}"></div><div class="row ir"><span style="width:30px"></span><span class="grow">总数 <b class="num">${o.totalMax}</b></span><div class="stepper"><button data-act="step" data-f="totalMax" data-d="-1">${ic('minus',18)}</button><button data-act="step" data-f="totalMax" data-d="1">${ic('plus',18)}</button></div></div>`;
    let g3=`<div class="row ir">${tile('chart','green')}<span class="grow">追踪指标</span><button class="switch ${o.hasMetric?'on':''}" data-act="tgl" data-f="hasMetric" aria-label="追踪指标"></button></div>`;
    if(o.hasMetric)g3+=[['mName','名称','体重',''],['mUnit','单位','kg',''],['mStart','当前','80','decimal'],['mTarget','目标','75','decimal']].map(([f,l,ph,im])=>`<div class="row ir"><span style="width:30px"></span><span class="grow">${l}</span><input class="ri" data-in="f" data-f="${f}" value="${esc(o[f])}" placeholder="${ph}" ${im?'inputmode="decimal"':''}></div>`).join('');
    h+=grp(g2)+grp(g3);
  }
  return h+grp(projRow(o)+spaceRow(o))}
IN.f=t=>{const o=tgt();if(!o)return;o[t.dataset.f]=t.value;if(t.classList.contains('ag'))grow(t);const s=cur();
  if(s.kind==='new'){const b=$('[data-act=add]',s.el);if(b)b.disabled=!(o.title.trim()||(o.kind==='note'&&o.body.trim()))}
  else if(s.kind==='note')autosave();else soft()};

/* ================= 新建 ================= */
const KINDS=[['todo','待办'],['event','日程'],['note','笔记'],['project','项目'],['goal','目标']];
const PH={todo:'要做什么',event:'日程标题',project:'项目名称',goal:'目标名称，如 健身'};
SC.new={title:'新建',tgt:()=>S.draft,white:()=>S.draft.kind==='note',
 center:(p,s)=>s.st.hint?`<div style="text-align:center;line-height:1.15"><div class="nav-title">新建</div><div class="nav-status" style="color:var(--ok);font-weight:600">草稿已保存</div></div>`:`<span class="nav-title">新建</span>`,
 init(p,s){const d=S.draft;if(d.title.trim()||d.body.trim())s.st.hint=true;
  if(p.kind)d.kind=p.kind;if(p.date)d.date=p.date;if(p.start){d.start=p.start;d.end=mhm(Math.min(hm(p.start)+60,1435));d.allDay=false}
  if(p.project){d.project=p.project;const pr=proj(p.project);if(pr)d.space=pr.space}s.st.fresh=true},
 right:()=>S.draft.kind==='note'?`<button class="navbtn b" data-act="add" ${S.draft.title.trim()||S.draft.body.trim()?'':'disabled'}>添加</button>`:'',
 body(p,s){const o=S.draft;let h=`<div class="newseg">${seg(KINDS,o.kind,'kind')}</div>`;
  if(o.kind==='note')return h+editorHTML(o,s,true);
  h+=`<div class="sec"><div class="group titlebox"><input class="big-in" data-in="f" data-f="title" placeholder="${PH[o.kind]}" value="${esc(o.title)}" enterkeyhint="done" autocomplete="off" ${s.st.fresh?'data-autofocus':''}></div></div>`;
  if(o.kind==='todo'||o.kind==='event'){const nm=addD(monday(TODAY),7);const qs=[['今天',TODAY],['明天',addD(TODAY,1)],['下周一',nm]];const other=o.date&&!qs.some(x=>x[1]===o.date);
    h+=`<div class="qd">${qs.map(([l,d])=>`<button class="chip ${o.date===d?'on':''}" data-act="qd" data-v="${d}">${l}</button>`).join('')}<button class="chip ${other?'on':''}" data-act="qd" data-v="pick">${other?md(o.date):'选择…'}</button>${o.kind==='todo'?`<button class="chip ${!o.date?'on':''}" data-act="qd" data-v="none">无日期</button>`:''}</div>`}
  if(o.kind==='todo')h+=todoForm(o,s);else if(o.kind==='event')h+=eventForm(o,s);else if(o.kind==='project')h+=projForm(o,s,true);else h+=goalForm(o,s,true);
  return h},
 bottom:(p,s)=>S.draft.kind==='note'?editorBar():`<button class="btn" data-act="add" ${S.draft.title.trim()?'':'disabled'}>添加${KINDS.find(k=>k[0]===S.draft.kind)[1]}</button>`,
 bcls:()=>S.draft.kind==='note'?'kbb':'',
 mount(p,s){s.st.fresh=false}};
ACT.kind=t=>{S.draft.kind=t.dataset.v;if(S.draft.kind==='event'&&!S.draft.date)S.draft.date=TODAY;const s=cur();s.st.exp=null;render(s)};
ACT.qd=t=>{const o=S.draft,s=cur(),v=t.dataset.v;if(v==='pick'){if(!o.date)o.date=TODAY;s.st.exp='date'}else if(v==='none'){o.date=null;o.time=null;s.st.exp=null}else{o.date=v;s.st.exp=null}s.st.mcm=null;render(s)};
ACT.add=()=>{const s=cur();if(s.kind!=='new')return;const o=S.draft;const title=o.title.trim();if(!title&&!(o.kind==='note'&&o.body.trim()))return;
  if(o.kind==='todo')S.todos.unshift(T(nid('t'),title,{date:o.date,time:o.date?o.time:null,remind:o.date?o.remind:'none',repeat:o.repeat,days:[...o.days],project:o.project,space:o.space,prio:o.prio,note:o.note}));
  else if(o.kind==='event')S.events.push(E(nid('e'),title,{date:o.date||TODAY,start:o.start,end:o.end,allDay:o.allDay,location:o.location,remind:o.remind,repeat:o.repeat,days:[...o.days],project:o.project,space:o.space,note:o.note}));
  else if(o.kind==='note')S.notes.unshift({id:nid('n'),title:title||stripMd(o.body).slice(0,14),space:o.space,project:o.project,tags:[...o.tags],pinned:false,updated:TODAY+' '+nowHM(),body:o.body});
  else if(o.kind==='project')S.projects.unshift({id:nid('p'),title,space:o.space,start:o.pStart,due:o.pDue,pinned:o.pinned,archived:false,note:o.note});
  else S.goals.push({id:nid('g'),title,space:o.space,project:o.project,period:o.period,unit:o.unit,target:+o.target,log:{},streak:0,dims:[],total:o.hasTotal?{name:o.totalName||'次数',cur:0,max:+o.totalMax||10}:null,
    metric:o.hasMetric&&parseFloat(o.mStart)?{name:o.mName||'指标',unit:o.mUnit,start:parseFloat(o.mStart),target:parseFloat(o.mTarget)||parseFloat(o.mStart),readings:[{date:TODAY,v:parseFloat(o.mStart)}]}:null});
  const k=o.kind;const lab=KINDS.find(x=>x[0]===k)[1];S.draft=newDraft();
  pop();refreshAll();hud('已添加'+lab)};

/* ================= note editor ================= */
function findRef(n){const u=x=>x.replace(/&amp;/g,'&').replace(/&lt;/g,'<').replace(/&gt;/g,'>').replace(/&quot;/g,'"').replace(/&#39;/g,"'");n=u(n);for(const k of ['p','t','e','n','g']){const o=S[KIND[k]].find(x=>x.title===n);if(o)return k+':'+o.id}return null}
function inl(s){s=esc(s);
  s=s.replace(/@\[([^\]]+)\](?:\(([ptengh]):(\w+)\))?/g,(m,n,k,id)=>{let ref=k?k+':'+id:findRef(n);if(!ref)return '@'+n;return `<span class="ref" data-act="openref" data-ref="${ref}">${ic({p:'folder',t:'checklist',e:'cal',n:'note',g:'target',h:'money'}[ref[0]],14)}${n}</span>`});
  s=s.replace(/`([^`]+)`/g,'<code>$1</code>').replace(/\*\*([^*]+)\*\*/g,'<b>$1</b>').replace(/(^|[^*])\*([^*\s][^*]*)\*/g,'$1<i>$2</i>');
  s=s.replace(/\[([^\]]+)\]\((https?:[^)\s]*)\)/g,'<span class="lnk" data-act="lnk" data-u="$2">$1</span>');
  s=s.replace(/(^|\s)#([\p{L}\p{N}_]+)/gu,'$1<span class="htag">#$2</span>');
  return s}
function mdRender(src){if(!src.trim())return'';const L=src.split('\n');let h='',code=null;
  L.forEach((ln,i)=>{let m;
    if(code){if(/^```/.test(ln)){h+=`<pre>${esc(code.join('\n'))}</pre>`;code=null}else code.push(ln);return}
    if(/^```/.test(ln)){code=[];return}
    if(m=ln.match(/^(#{1,3})\s+(.*)/))h+=`<h${m[1].length+1}>${inl(m[2])}</h${m[1].length+1}>`;
    else if(m=ln.match(/^\s*[-*]\s+\[( |x|X)\]\s?(.*)/)){const on=m[1]!==' ';h+=`<div class="cl ${on?'on':''}"><button class="ckb" data-act="mdchk" data-line="${i}" aria-label="勾选"><span class="ck sm ${on?'on':''}">${on?ic('check',12):''}</span></button><span>${inl(m[2])}</span></div>`}
    else if(m=ln.match(/^\s*[-*]\s+(.*)/))h+=`<div class="li"><i></i><span>${inl(m[1])}</span></div>`;
    else if(m=ln.match(/^\s*(\d+)\.\s+(.*)/))h+=`<div class="li"><em>${m[1]}.</em><span>${inl(m[2])}</span></div>`;
    else if(m=ln.match(/^>\s?(.*)/))h+=`<blockquote>${inl(m[1])}</blockquote>`;
    else if(!ln.trim())h+='<div class="gap"></div>';
    else h+=`<p>${inl(ln)}</p>`});
  if(code)h+=`<pre>${esc(code.join('\n'))}</pre>`;return h}
function editorHTML(o,s,isNew){const mode=isNew?'edit':(s.st.mode||'preview');const p=o.project?proj(o.project):null;
  let h=`<div class="edmeta"><button class="pill" data-act="pick" data-f="space">${dot(o.space)}${SPN[o.space]}</button><button class="pill" data-act="pick" data-f="project">${ic('folder',14)}${p?esc(p.title):'无项目'}</button>${o.tags.map(t=>`<button class="pill acc" data-act="tagmenu">#${esc(t)}</button>`).join('')}<button class="pill" data-act="tagmenu" aria-label="标签">${ic('tag',14)}${o.tags.length?'':'标签'}</button></div>`;
  h+=`<input class="ed-title" data-in="f" data-f="title" placeholder="标题" value="${esc(o.title)}" autocomplete="off" ${isNew&&s.st.fresh&&S.draft.kind==='note'?'data-autofocus':''}>`;
  if(mode==='edit')h+=`<textarea class="ed-body ag" data-in="body" placeholder="开始书写…">${esc(o.body)}</textarea>`;
  else h+=`<div class="md" data-act="ededit">${mdRender(o.body)||'<p class="muted">轻点开始书写</p>'}</div>`;
  return h}
function editorBar(){const ks=[['h','<b style="font-size:18px">H</b>'],['b','<b style="font-size:18px">B</b>'],['i','<i style="font-family:Georgia,serif;font-size:20px">I</i>'],['ul',ic('list',22)],['cl',ic('checklist',22)],['q',ic('quote',20)],['code',ic('code',22)],['link',ic('link',21)],['at',ic('at',22)]];
  return `<div class="atp" hidden></div><div class="kbbar">${ks.map(([k,v])=>`<div class="k" role="button" tabindex="-1" data-act="kb" data-k="${k}">${v}</div>`).join('')}<div class="k done" role="button" tabindex="-1" data-act="kbdone" aria-label="收起键盘">${ic('kbd',22)}</div></div>`}
const statusText=n=>n?'已保存 '+relTime(n.updated):'';
function autosave(){const s=cur();const o=tgt();if(!o)return;if(s.kind==='new'){const b=$('[data-act=add]',s.el);if(b)b.disabled=!(o.title.trim()||o.body.trim());return}
  o.updated=TODAY+' '+nowHM();const st=$('[data-status]',s.el);if(st){st.textContent='正在保存…';clearTimeout(s._sv);s._sv=setTimeout(()=>{st.textContent='已保存 '+nowHM()},700)}soft()}
IN.body=t=>{const o=tgt();o.body=t.value;grow(t);autosave();atCheck(t)};
function atCheck(ta){const s=cur();const pn=$('.atp',s.el);if(!pn)return;const before=ta.value.slice(0,ta.selectionStart);const m=before.match(/(^|[\s(（，。])@([^\s@\[\]()]{0,12})$/);
  if(!m){pn.hidden=true;$$('.kbbar .k[data-k=at]',s.el).forEach(x=>x.classList.remove('on'));return}
  pn.innerHTML=atList(m[2]);pn.hidden=false;$$('.kbbar .k[data-k=at]',s.el).forEach(x=>x.classList.add('on'))}
function atList(q){const self=cur().p.id;const ql=q.toLowerCase();let rows=[];
  S.projects.filter(p=>!p.archived).forEach(p=>{const kids=[...S.todos.filter(t=>t.project===p.id&&!t.done).map(t=>['t',t.id,t.title,'checklist','待办']),...S.events.filter(e=>e.project===p.id).map(e=>['e',e.id,e.title,'cal','日程']),...S.notes.filter(n=>n.project===p.id&&n.id!==self).map(n=>['n',n.id,n.title,'note','笔记']),...S.goals.filter(g=>g.project===p.id).map(g=>['g',g.id,g.title,'target','目标'])];
    const pm=!q||p.title.toLowerCase().includes(ql);const km=pm?kids:kids.filter(k=>k[2].toLowerCase().includes(ql));
    if(pm||km.length){rows.push(`<button data-act="atpick" data-ref="p:${p.id}" data-name="${esc(p.title)}"><span style="color:var(--${p.space})">${ic('folder',20)}</span><div class="grow"><div class="t1">${esc(p.title)}</div><div class="t2">项目 · ${kids.length} 项</div></div></button>`);
      km.slice(0,8).forEach(k=>rows.push(`<button class="c" data-act="atpick" data-ref="${k[0]}:${k[1]}" data-name="${esc(k[2])}"><span style="color:var(--label2)">${ic(k[3],18)}</span><div class="grow"><div class="t1">${esc(k[2])}</div><div class="t2">${k[4]}</div></div></button>`))}});
  return `<div class="h">引用${q?' “'+esc(q)+'”':''}</div>`+(rows.join('')||'<div class="empty">没有匹配</div>')}
ACT.atpick=t=>{const s=cur();const ta=$('.ed-body',s.el);if(!ta)return;const c=ta.selectionStart;const m=ta.value.slice(0,c).match(/@([^\s@\[\]()]{0,12})$/);const st=m?c-m[0].length:c;
  ta.focus();ta.setRangeText(`@[${t.dataset.name}] `,st,c,'end');IN.body(ta)};
ACT.kb=t=>{const s=cur();const ta=$('.ed-body',s.el);if(!ta)return;const k=t.dataset.k;ta.focus();const v=ta.value,a=ta.selectionStart,b=ta.selectionEnd,sel=v.slice(a,b);const ls=v.lastIndexOf('\n',a-1)+1;
  const lp=(pfx,re)=>{const m=v.slice(ls).match(re);if(m)ta.setRangeText('',ls,ls+m[0].length,'preserve');else ta.setRangeText(pfx,ls,ls,'preserve');if(!m&&a===b)ta.setSelectionRange(a+pfx.length,a+pfx.length)};
  if(k==='h')lp('## ',/^#{1,3} /);else if(k==='ul')lp('- ',/^- (?!\[)/);else if(k==='cl')lp('- [ ] ',/^- \[[ xX]\] /);else if(k==='q')lp('> ',/^> /);
  else if(k==='b'||k==='i'||k==='code'){const w=k==='b'?'**':k==='i'?'*':'`';ta.setRangeText(w+sel+w,a,b,'end');if(!sel)ta.setSelectionRange(a+w.length,a+w.length)}
  else if(k==='link'){const tx=sel||'链接';ta.setRangeText(`[${tx}](https://)`,a,b,'end');const u=a+tx.length+3;ta.setSelectionRange(u,u+8)}
  else if(k==='at'){const pn=$('.atp',s.el);if(pn&&!pn.hidden){pn.hidden=true;t.classList.remove('on');return}const pre=a>0&&!/\s/.test(v[a-1])?' @':'@';ta.setRangeText(pre,a,b,'end')}
  IN.body(ta)};
ACT.kbdone=()=>{const s=cur();if(document.activeElement)document.activeElement.blur();const pn=$('.atp',s.el);if(pn)pn.hidden=true;if(s.kind==='note'){s.st.mode='preview';render(s)}};
ACT.ededit=(t,e)=>{if(e&&e.target.closest('.ref,.ckb,.lnk'))return;const s=cur();s.st.mode='edit';render(s);const ta=$('.ed-body',s.el);if(ta){ta.focus();ta.setSelectionRange(ta.value.length,ta.value.length)}};
ACT.edpreview=()=>{const s=cur();if(document.activeElement)document.activeElement.blur();s.st.mode='preview';render(s)};
ACT.mdchk=t=>{const o=tgt();const L=o.body.split('\n');const i=+t.dataset.line;L[i]=L[i].replace(/\[( |x|X)\]/,m=>m==='[ ]'?'[x]':'[ ]');o.body=L.join('\n');render(cur());autosave()};
ACT.openref=t=>openRef(t.dataset.ref);
ACT.lnk=t=>hud(t.dataset.u,'link');
ACT.tagmenu=t=>{const o=tgt();const all=[...new Set(S.notes.flatMap(n=>n.tags).concat(o.tags))];
  menu(t,[...all.map(tg=>({label:'#'+tg,checked:o.tags.includes(tg),fn:()=>{o.tags=o.tags.includes(tg)?o.tags.filter(x=>x!==tg):[...o.tags,tg];render(cur());if(cur().kind!=='new')autosave()}})),'-',{label:'新标签…',icon:'plus',fn:()=>sheet({title:'新标签',body:`<div class="group"><input class="fs-in" data-autofocus placeholder="标签名" autocomplete="off"></div>`,onDone:w=>{const v=$('.fs-in',w).value.trim().replace(/^#/,'');if(!v)return false;if(!o.tags.includes(v))o.tags.push(v);render(cur());if(cur().kind!=='new')autosave()}})}])};
SC.note={title:p=>{const n=get('n:'+p.id);return n?(n.title||'笔记'):'笔记'},tgt:p=>get('n:'+p.id),gone:p=>!get('n:'+p.id),cls:'white',
 init(p,s){s.st.mode='preview'},
 center:p=>`<span class="nav-status" data-status>${statusText(get('n:'+p.id))}</span>`,
 right:(p,s)=>s.st.mode==='edit'?`<button class="navbtn b" data-act="edpreview">完成</button>`:`<button class="navbtn" data-act="notemore" aria-label="更多">${ic('more',24)}</button>`,
 body:(p,s)=>editorHTML(get('n:'+p.id),s,false),
 bottom:(p,s)=>s.st.mode==='edit'?editorBar():'',bcls:()=>'kbb'};
ACT.notemore=t=>{const n=tgt();menu(t,[{label:n.pinned?'取消置顶':'置顶',icon:'pinned',fn:()=>{n.pinned=!n.pinned;refreshAll();hud(n.pinned?'已置顶':'已取消置顶','pinned')}},{label:'编辑',icon:'pencil',fn:()=>ACT.ededit()},'-',{label:'删除笔记',icon:'trash',danger:true,fn:()=>confirmDel('删除笔记？','“'+(n.title||'无标题')+'”',()=>{S.notes.splice(S.notes.indexOf(n),1);pop();refreshAll();hud('已删除','trash')})}])};
