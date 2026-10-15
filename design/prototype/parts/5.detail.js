
/* ================= 待办 ================= */
function progCard(o,s){const tot=o.sittings.reduce((a,x)=>a+x.min,0);
  let h=`<div class="sec"><div class="card"><div class="pc-top"><div class="pbig mono"><span class="${S._animP===o.id?'bump':''}">${o.progress}</span><small>%</small></div><div class="pc-meta">${o.sittings.length?`已做 ${o.sittings.length} 次<br>共 ${dur(tot)}`:'进度'}</div></div><div class="bar lg"><i style="width:${o.progress}%;${o.progress>=100?'background:var(--ok)':''}"></i></div>`;
  if(s.st.log){h+=`<div class="lf"><div class="lf-l"><span>这次用时</span></div><div class="chips">${[15,30,60,120].map(m=>`<button class="chip f ${s.st.lmin===m?'on':''}" data-act="lmin" data-v="${m}">${m<60?m+' 分':(m/60)+' 小时'}</button>`).join('')}</div><div class="lf-l"><span>完成到</span><b class="mono" data-lp style="color:var(--label);font-size:15px">${s.st.lpct}%</b></div><input class="slider" type="range" min="0" max="100" step="5" value="${s.st.lpct}" data-in="lpct" aria-label="进度"><input class="lf-note" placeholder="做了什么" value="${esc(s.st.lnote||'')}" data-in="lnote" autocomplete="off"><div class="lf-acts"><button class="btn ghost sm" data-act="logx">取消</button><button class="btn sm" data-act="logsave">记录</button></div></div>`}
  else h+=`<button class="btn tint sm" style="margin-top:14px" data-act="logopen">${ic('plus',18)}记一次</button>`;
  return h+'</div></div>'}
SC.todo={title:'待办',tgt:p=>get('t:'+p.id),gone:p=>!get('t:'+p.id),
 init(p,s){s.st.lmin=30},
 right:()=>`<button class="navbtn" data-act="todomore" aria-label="更多">${ic('more',24)}</button>`,
 body(p,s){const o=get('t:'+p.id);const pr=o.project?proj(o.project):null;
  let h=`<div class="hdr"><button class="ckb" data-act="tdone" data-id="${o.id}" aria-label="完成"><span class="ck lg ${o.done?'on':''} ${S._animT===o.id?'pop':''}">${o.done?ic('check',16):''}</span></button><textarea class="hdr-title ag ${o.done?'done':''}" rows="1" data-in="f" data-f="title" placeholder="标题">${esc(o.title)}</textarea></div>`;
  h+=`<div class="hmeta"><span class="sptag ${o.space}">${dot(o.space)}${SPN[o.space]}</span>${pr?`<button class="pill" data-act="open" data-ref="p:${pr.id}">${ic('folder',14)}${esc(pr.title)}${ic('chev',12)}</button>`:''}${o.date?`<span class="muted" style="font-size:15px">${rel(o.date)}${o.time?' '+o.time:''}</span>`:''}</div>`;
  h+=progCard(o,s);
  if(o.sittings.length)h+=grpH('记录',o.sittings.slice().reverse().map(x=>`<div class="r"><div class="grow"><div>${mdw(x.date)}</div>${x.note?`<div style="font-size:13px;color:var(--label2);margin-top:1px">${esc(x.note)}</div>`:''}</div><div class="v num">${x.min} 分钟<small>→ ${x.pct}%</small></div></div>`).join(''),'hist');
  return h+todoForm(o,s)}};
ACT.tdone=t=>{const o=get('t:'+t.dataset.id);if(!o)return;o.done=!o.done;S._animT=o.done?o.id:null;refreshAll();S._animT=null};
ACT.logopen=()=>{const s=cur(),o=tgt();s.st.log=true;s.st.lpct=Math.min(100,o.progress+20);s.st.lnote='';s.st.lmin=s.st.lmin||30;render(s)};
ACT.logx=()=>{const s=cur();s.st.log=false;render(s)};
ACT.lmin=t=>{cur().st.lmin=+t.dataset.v;ACT.chip(t)};
IN.lpct=t=>{const s=cur();s.st.lpct=+t.value;t.style.setProperty('--v',t.value+'%');const l=$('[data-lp]',s.el);if(l)l.textContent=t.value+'%'};
IN.lnote=t=>{cur().st.lnote=t.value};
ACT.logsave=()=>{const s=cur(),o=tgt();const m=s.st.lmin||30;o.sittings.push({date:TODAY,min:m,pct:s.st.lpct,note:(s.st.lnote||'').trim()});o.progress=s.st.lpct;
  if(o.progress>=100)o.done=true;s.st.log=false;S._animP=o.id;refreshAll();S._animP=null;hud(o.done?'已完成 · 记录 '+dur(m):'已记录 '+dur(m))};
ACT.todomore=t=>{const o=tgt();menu(t,[{label:o.done?'标为未完成':'完成',icon:'check',fn:()=>{o.done=!o.done;refreshAll();if(o.done)hud('已完成')}},{label:'推迟到明天',icon:'later',fn:()=>{o.date=addD(o.date&&o.date>TODAY?o.date:TODAY,1);refreshAll();hud('已推迟到'+rel(o.date),'later')}},'-',{label:'删除',icon:'trash',danger:true,fn:()=>confirmDel('删除待办？','“'+o.title+'”',()=>{S.todos.splice(S.todos.indexOf(o),1);pop();refreshAll();hud('已删除','trash')})}])};

/* ================= 日程 ================= */
SC.event={title:'日程',tgt:p=>get('e:'+p.id),gone:p=>!get('e:'+p.id),
 body(p,s){const o=get('e:'+p.id);const pr=o.project?proj(o.project):null;
  let h=`<div class="hdr"><span style="width:5px;align-self:stretch;border-radius:3px;background:var(--${o.space});margin:4px 2px 4px 0"></span><textarea class="hdr-title ag" rows="1" data-in="f" data-f="title" placeholder="标题">${esc(o.title)}</textarea></div>`;
  h+=`<div class="hmeta"><span class="sptag ${o.space}">${dot(o.space)}${SPN[o.space]}</span>${pr?`<button class="pill" data-act="open" data-ref="p:${pr.id}">${ic('folder',14)}${esc(pr.title)}${ic('chev',12)}</button>`:''}<span class="muted" style="font-size:15px">${rel(o.date)}${o.allDay?' 全天':' '+o.start+' – '+o.end}</span></div>`;
  return h+eventForm(o,s)+grp(`<button class="row danger" data-act="evdel">删除日程</button>`)}};
ACT.evdel=()=>{const o=tgt();const del=()=>{S.events.splice(S.events.indexOf(o),1);pop();refreshAll();hud('已删除','trash')};
  if(o.repeat!=='none')actionSheet({title:'这是重复日程',actions:[{label:'仅删除此次',danger:true,fn:del},{label:'删除所有将来日程',danger:true,fn:del}]});
  else actionSheet({title:'删除“'+o.title+'”？',actions:[{label:'删除日程',danger:true,fn:del}]})};

/* ================= 项目 ================= */
function pTodoRow(t){const sub=[t.date?(!t.done&&t.date<TODAY?`<span class="red">${rel(t.date)}</span>`:rel(t.date)):'',t.progress&&!t.done?t.progress+'%':''].filter(Boolean).join(' · ');
  const inner=`<div class="row tap" data-act="open" data-ref="t:${t.id}" style="min-height:52px;padding-right:4px"><button class="ckb" data-act="tdone" data-id="${t.id}" aria-label="完成"><span class="ck ${t.done?'on':''} ${S._animT===t.id?'pop':''}">${t.done?ic('check',14):''}</span></button><div class="grow"><div class="t1" style="${t.done?'color:var(--label2);text-decoration:line-through':''}">${t.prio?`<span class="red" style="font-weight:700">${PRIO[t.prio]} </span>`:''}${esc(t.title)}</div>${sub?`<div class="t2">${sub}</div>`:''}</div><button class="navbtn" style="color:var(--label3)" data-act="itemmore" data-ref="t:${t.id}" aria-label="更多">${ic('dots',22)}</button></div>`;
  return swRow('t:'+t.id,inner,t.done?'':'done','del','pli')}
const moreBtn=ref=>`<button class="navbtn" style="color:var(--label3)" data-act="itemmore" data-ref="${ref}" aria-label="更多">${ic('dots',22)}</button>`;
SC.project={title:p=>proj(p.id)?proj(p.id).title:'项目',tgt:p=>proj(p.id),gone:p=>!proj(p.id),
 right:()=>`<button class="navbtn" data-act="projmore" aria-label="更多">${ic('more',24)}</button>`,
 body(p){const o=proj(p.id);const s=pStats(o);const pc=s.total?s.done/s.total:0;
  let h=`<div class="phead"><span class="sptag ${o.space}">${dot(o.space)}${SPN[o.space]}</span>${o.pinned?` <span class="sptag" style="background:var(--fill);color:var(--label2)">${ic('pinned',12)}置顶</span>`:''}${o.archived?' <span class="sptag" style="background:var(--fill);color:var(--label2)">已归档</span>':''}<h1>${esc(o.title)}</h1><div class="d num">${md(o.start)} – ${md(o.due)}${o.note?' · '+esc(o.note):''}</div></div>`;
  h+=`<div class="sec"><div class="card"><div class="pc-top"><div class="pbig mono">${s.done}<small>/${s.total} 待办</small></div><div class="pc-meta">${s.left>=0?`还剩 <b class="num" style="color:var(--label);font-size:22px">${s.left}</b> 天`:'已过截止日'}<br>截止 ${mdw(o.due)}</div></div><div class="bar lg"><i style="width:${pc*100}%;background:var(--${o.space})"></i></div>${s.next?`<div class="kv" style="margin-top:12px"><span>下一步</span><span>${esc(s.next.title)}${s.next.date?' · '+rel(s.next.date):''}</span></div>`:''}</div></div>`;
  const ts=S.todos.filter(t=>t.project===o.id).sort((a,b)=>(a.done-b.done)||byDate(a,b));
  const es=S.events.filter(e=>e.project===o.id).sort((a,b)=>(a.date+a.start).localeCompare(b.date+b.start));
  const ns=S.notes.filter(n=>n.project===o.id);const gs=S.goals.filter(g=>g.project===o.id);
  h+=grpH(`待办 · ${ts.length}`,ts.map(pTodoRow).join('')||'<div class="empty">没有待办</div>');
  if(es.length)h+=grpH(`日程 · ${es.length}`,es.map(e=>`<div class="row tap pli" data-act="open" data-ref="e:${e.id}" style="padding-right:4px;min-height:52px"><span style="width:24px;display:grid;place-items:center;color:var(--${e.space})">${ic('cal',20)}</span><div class="grow"><div class="t1">${esc(e.title)}</div><div class="t2">${mdw(e.date)}${e.allDay?' 全天':' '+e.start}${e.location?' · '+esc(e.location):''}</div></div>${moreBtn('e:'+e.id)}</div>`).join(''));
  if(ns.length)h+=grpH(`笔记 · ${ns.length}`,ns.map(n=>`<div class="row tap pli" data-act="open" data-ref="n:${n.id}" style="padding-right:4px;min-height:52px"><span style="width:24px;display:grid;place-items:center;color:var(--warn)">${ic('note',20)}</span><div class="grow"><div class="t1">${esc(n.title)}</div><div class="t2">${esc(stripMd(n.body))}</div></div>${moreBtn('n:'+n.id)}</div>`).join(''));
  if(gs.length)h+=grpH(`目标 · ${gs.length}`,gs.map(g=>{const q=gProg(g);return `<div class="row tap pli" data-act="open" data-ref="g:${g.id}" style="padding-right:4px;min-height:52px"><span style="width:24px">${ring(q.c/q.t,24,3.5,'var(--accent)')}</span><div class="grow"><div class="t1">${esc(g.title)}</div><div class="t2">${q.c}/${q.t} ${PER[g.period]}</div></div>${moreBtn('g:'+g.id)}</div>`}).join(''));
  return h},
 bottom:()=>`<button class="btn" data-act="projadd">${ic('plus',20)}添加</button>`};
ACT.projadd=t=>{const id=cur().p.id;menu(t,[{label:'新建待办',icon:'checklist',fn:()=>push('new',{kind:'todo',project:id})},{label:'新建日程',icon:'cal',fn:()=>push('new',{kind:'event',project:id})},{label:'新建笔记',icon:'note',fn:()=>push('new',{kind:'note',project:id})},{label:'新建目标',icon:'target',fn:()=>push('new',{kind:'goal',project:id})},'-',{label:'关联已有…',icon:'link',fn:()=>push('link',{id})}])};
ACT.itemmore=t=>{const ref=t.dataset.ref;const o=get(ref);if(!o)return;const nm={t:'待办',e:'日程',n:'笔记',g:'目标'}[ref[0]];
  menu(t,[{label:'移出项目',icon:'folder',fn:()=>{o.project=null;refreshAll();hud('已移出项目')}},'-',{label:'删除'+nm,icon:'trash',danger:true,fn:()=>confirmDel('删除'+nm+'？','“'+o.title+'”',()=>{const a=S[KIND[ref[0]]];a.splice(a.indexOf(o),1);refreshAll();hud('已删除','trash')})}])};
function delProj(p,all){S.projects.splice(S.projects.indexOf(p),1);['todos','events','notes','goals'].forEach(k=>{if(all)S[k]=S[k].filter(x=>x.project!==p.id);else S[k].forEach(x=>{if(x.project===p.id)x.project=null})});pop();refreshAll();hud(all?'已删除项目及内容':'已删除项目','trash')}
ACT.projmore=t=>{const p=tgt();const n=['todos','events','notes','goals'].reduce((a,k)=>a+S[k].filter(x=>x.project===p.id).length,0);
  menu(t,[{label:'编辑',icon:'pencil',fn:()=>push('projedit',{id:p.id})},{label:p.pinned?'取消置顶':'置顶',icon:'pinned',fn:()=>{p.pinned=!p.pinned;refreshAll();hud(p.pinned?'已置顶':'已取消置顶','pinned')}},{label:p.archived?'取消归档':'归档',icon:'archive',fn:()=>{p.archived=!p.archived;if(p.archived){pop();refreshAll();hud('已归档','archive')}else{refreshAll();hud('已取消归档','archive')}}},'-',
    {label:'删除项目',icon:'trash',danger:true,fn:()=>actionSheet({title:'删除“'+p.title+'”？',msg:n?`项目里有 ${n} 项内容`:'',actions:n?[{label:'只删项目，保留内容',danger:true,fn:()=>delProj(p,false)},{label:'连同内容一起删除',danger:true,fn:()=>delProj(p,true)}]:[{label:'删除项目',danger:true,fn:()=>delProj(p,false)}]})}])};
SC.projedit={title:'编辑项目',tgt:p=>proj(p.id),gone:p=>!proj(p.id),
 right:()=>`<button class="navbtn b" data-act="back">完成</button>`,
 body(p,s){const o=proj(p.id);return `<div class="sec"><div class="group titlebox"><input class="big-in" data-in="f" data-f="title" value="${esc(o.title)}" placeholder="项目名称" autocomplete="off"></div></div>`+projForm(o,s,false)}};

/* ================= 关联已有 ================= */
function linkCands(pid,k,q){const arr={t:S.todos.filter(t=>!t.done),e:S.events,n:S.notes,g:S.goals}[k].filter(x=>x.project!==pid);const ql=q.trim().toLowerCase();return ql?arr.filter(x=>x.title.toLowerCase().includes(ql)):arr}
function linkList(p,s){const k=s.st.k;const xs=linkCands(p.id,k,s.st.q);if(!xs.length)return '<div class="empty">没有可关联的内容</div>';
  return `<div class="group">${xs.map(x=>{const ref=k+':'+x.id;const on=s.st.sel.has(ref);const op=x.project?proj(x.project):null;
    const sub=[k==='t'&&x.date?rel(x.date):'',k==='e'?mdw(x.date):'',k==='n'?relTime(x.updated):'',op?'在「'+esc(op.title)+'」':''].filter(Boolean).join(' · ');
    return `<button class="row pli" data-act="linktg" data-ref="${ref}" style="min-height:52px"><span class="ck ${on?'on':''}">${on?ic('check',14):''}</span><div class="grow"><div class="t1">${esc(x.title)}</div>${sub?`<div class="t2">${sub}</div>`:''}</div>${dot(x.space)}</button>`}).join('')}</div>`}
SC.link={title:'关联已有',init(p,s){s.st.k='t';s.st.sel=new Set();s.st.q=''},
 body(p,s){return `<div class="newseg">${seg([['t','待办'],['e','日程'],['n','笔记'],['g','目标']],s.st.k,'linkk')}</div><div class="search">${ic('search',18)}<input type="search" placeholder="搜索" value="${esc(s.st.q)}" data-in="linkq" autocomplete="off"></div><div class="sec ll">${linkList(p,s)}</div>`},
 bottom:(p,s)=>{const n=s.st.sel.size;return `<button class="btn" data-act="linkdo" ${n?'':'disabled'}>${n?'关联 '+n+' 项':'关联'}</button>`}};
ACT.linkk=t=>{const s=cur();s.st.k=t.dataset.v;render(s)};
IN.linkq=t=>{const s=cur();s.st.q=t.value;$('.ll',s.el).innerHTML=linkList(s.p,s)};
ACT.linktg=t=>{const s=cur();const r=t.dataset.ref;const ck=$('.ck',t);if(s.st.sel.has(r)){s.st.sel.delete(r);ck.classList.remove('on');ck.innerHTML=''}else{s.st.sel.add(r);ck.classList.add('on','pop');ck.innerHTML=ic('check',14)}
  const b=$('[data-act=linkdo]',s.el);const n=s.st.sel.size;b.disabled=!n;b.textContent=n?'关联 '+n+' 项':'关联'};
ACT.linkdo=()=>{const s=cur();const pid=s.p.id;let n=0;s.st.sel.forEach(r=>{const o=get(r);if(o){o.project=pid;n++}});pop();refreshAll();hud('已关联 '+n+' 项','link')};

/* ================= 目标 ================= */
function chart(m){const W=340,H=160,pl=30,pr=12,pt=14,pb=24;const rs=m.readings;const vals=rs.map(r=>r.v).concat([m.target,m.start]);const mn=Math.floor(Math.min(...vals)-.5),mx=Math.ceil(Math.max(...vals)+.3);
  const x=i=>pl+(rs.length===1?.5:i/(rs.length-1))*(W-pl-pr),y=v=>pt+(mx-v)/(mx-mn||1)*(H-pt-pb);
  let g='';for(let k=0;k<=3;k++){const v=mn+(mx-mn)*k/3;g+=`<line x1="${pl}" x2="${W-pr}" y1="${y(v)}" y2="${y(v)}" stroke="rgba(60,60,67,.12)"/><text x="${pl-6}" y="${y(v)+3}" text-anchor="end">${Math.round(v*10)/10}</text>`}
  const ty=y(m.target);g+=`<line x1="${pl}" x2="${W-pr}" y1="${ty}" y2="${ty}" stroke="var(--ok)" stroke-dasharray="4 4" stroke-width="1.2"/><text x="${W-pr}" y="${ty-5}" text-anchor="end" style="fill:var(--ok)">目标 ${m.target}</text>`;
  const pts=rs.map((r,i)=>[x(i),y(r.v)]);const line=pts.map(p=>p.join(',')).join(' ');
  const area=`M${pts[0][0]},${H-pb} `+pts.map(p=>'L'+p.join(',')).join(' ')+` L${pts[pts.length-1][0]},${H-pb} Z`;
  return `<svg class="chart" viewBox="0 0 ${W} ${H}" role="img" aria-label="${m.name}变化"><defs><linearGradient id="ga" x1="0" x2="0" y1="0" y2="1"><stop offset="0" stop-color="var(--accent)" stop-opacity=".18"/><stop offset="1" stop-color="var(--accent)" stop-opacity="0"/></linearGradient></defs>${g}<path d="${area}" fill="url(#ga)"/><polyline points="${line}" fill="none" stroke="var(--accent)" stroke-width="2.2" stroke-linejoin="round" stroke-linecap="round"/>${pts.map((p,i)=>`<circle cx="${p[0]}" cy="${p[1]}" r="${i===pts.length-1?4.5:3}" fill="${i===pts.length-1?'var(--accent)':'#fff'}" stroke="var(--accent)" stroke-width="2"/>`).join('')}<text x="${pts[0][0]}" y="${H-6}" text-anchor="start">${md(rs[0].date)}</text>${rs.length>1?`<text x="${pts[pts.length-1][0]}" y="${H-6}" text-anchor="end">${md(rs[rs.length-1].date)}</text>`:''}</svg>`}
SC.goal={title:p=>{const g=get('g:'+p.id);return g?g.title:'目标'},tgt:p=>get('g:'+p.id),gone:p=>!get('g:'+p.id),
 right:()=>`<button class="navbtn" data-act="goalmore" aria-label="更多">${ic('more',24)}</button>`,
 body(p){const g=get('g:'+p.id);const q=gProg(g);const a=S._anim&&S._anim.id===g.id?S._anim:null;const done=q.c>=q.t;const pr=g.project?proj(g.project):null;
  let h=`<div class="phead" style="padding-bottom:10px"><span class="sptag ${g.space}">${dot(g.space)}${SPN[g.space]}</span>${pr?` <button class="pill" style="height:24px;font-size:13px;border-radius:12px" data-act="open" data-ref="p:${pr.id}">${ic('folder',12)}${esc(pr.title)}</button>`:''}<h1>${esc(g.title)}</h1><div class="d">${g.dims.length?g.dims.map(d=>`${d.title} ${PERN[d.period]} ${d.target} 次`).join(' · '):`${PERN[g.period]} ${g.target} ${gU(g)}`}</div></div>`;
  const days=[6,5,4,3,2,1,0].map(i=>addD(TODAY,-i));
  h+=`<div class="sec"><div class="card ringbox"><div class="ringwrap">${ring(q.c/q.t,168,14,done?'var(--ok)':'var(--accent)',a?a.from:null)}<div class="c"><b class="mono"><span class="${a?'bump':''}">${q.c}</span><small style="font-size:20px;color:var(--label2);font-weight:500">/${q.t}</small></b><span>${PER[g.period]}${g.unit==='min'&&!g.dims.length?' · 分钟':''}</span></div></div>
    <button class="btn ${done?'okb':''}" style="margin-top:18px" data-act="gcheck" data-id="${g.id}">${done?ic('check',20)+'本期已达成 · 再记一次':(g.dims.length?ic('plus',20)+'打卡':(g.unit==='min'?ic('clock',20)+'记录时长':ic('plus',20)+'打卡 +1'))}</button>
    <div class="dots7">${days.map(d=>{const on=dayHas(g,d);return `<div class="${d===TODAY?'td':''}"><i class="${on?'on':''}">${on?ic('check',12):''}</i>${d===TODAY?'今':WD[s2d(d).getDay()]}</div>`}).join('')}</div></div></div>`;
  const monthCnt=g.dims.length?g.dims.reduce((x,d)=>x+cnt(d.log,'month'),0):cnt(g.log,'month');
  h+=`<div class="sec statrow"><div class="stat"><div class="k">连续达成</div><div class="v"><span class="streak" style="font-size:22px">${ic('flame',18)}${g.streak}</span> <small style="font-size:15px;color:var(--label2);font-weight:500">${PERU[g.period]}</small></div></div><div class="stat"><div class="k">本月</div><div class="v num">${monthCnt} <small style="font-size:15px;color:var(--label2);font-weight:500">${g.unit==='min'&&!g.dims.length?'分钟':'次'}</small></div></div></div>`;
  if(g.total)h+=`<div class="sec"><div class="card"><div class="pc-top" style="margin-bottom:10px"><div><div class="muted" style="font-size:13px">累计${g.total.name}</div><div class="pbig mono" style="font-size:34px;line-height:40px">${g.total.cur}<small>/${g.total.max}</small></div></div><div class="stepper"><button data-act="tot" data-d="-1" aria-label="减少">${ic('minus',18)}</button><button data-act="tot" data-d="1" aria-label="增加">${ic('plus',18)}</button></div></div><div class="bar lg"><i style="width:${g.total.cur/g.total.max*100}%"></i></div><div class="sf" style="padding:8px 0 0">还差 ${g.total.max-g.total.cur} ${g.total.name}</div></div></div>`;
  h+=grpH('维度',g.dims.map(d=>{const c=cnt(d.log,d.period);const dn=c>=d.target;const an=a&&a.dim===d.id;return `<div class="row tli" style="min-height:58px"><div class="grow-ring" style="width:40px">${ring(c/d.target,40,4.5,dn?'var(--ok)':'var(--accent)',an?a.dfrom:null)}<b>${dn?ic('check',14):''}</b></div><div class="grow"><div class="t1">${esc(d.title)}</div><div class="t2"><span class="${an?'bump':''}">${c}/${d.target}</span> ${PER[d.period]}</div></div><button class="cbtn ${dn?'done':''} ${an?'pop':''}" data-act="dcheck" data-d="${d.id}" aria-label="打卡">${dn?ic('check',20):'+1'}</button></div>`}).join('')+`<button class="row center" data-act="adddim">${ic('plus',18)}添加维度</button>`);
  if(g.metric){const m=g.metric;const rs=m.readings;const last=rs[rs.length-1].v;const dlt=Math.round((last-m.start)*10)/10;const left=Math.round((last-m.target)*10)/10;
    h+=`<div class="sec"><div class="sh">指标</div><div class="card"><div class="pc-top" style="margin-bottom:6px"><div><div class="muted" style="font-size:13px">${esc(m.name)}</div><div class="pbig mono" style="font-size:34px;line-height:40px">${last}<small>${esc(m.unit)}</small></div></div><div class="pc-meta">起始 ${m.start} · 目标 ${m.target}<br><b class="${dlt<=0?'green':'red'}">${dlt<=0?'已减 '+(-dlt):'增加 '+dlt} ${esc(m.unit)}</b>${left>0?' · 还差 '+left:''}</div></div>${chart(m)}<button class="btn tint sm" style="margin-top:10px" data-act="addread">${ic('plus',18)}记一次${esc(m.name)}</button></div></div>`;
    h+=grpH('记录',rs.slice().reverse().map((r,i,arr)=>{const prev=arr[i+1];const d=prev?Math.round((r.v-prev.v)*10)/10:null;return `<div class="row"><span class="grow">${mdw(r.date)}</span><span class="mono" style="font-size:15px">${r.v} ${esc(m.unit)}</span><span class="mono ${d==null?'muted':d<=0?'green':'red'}" style="width:56px;text-align:right;font-size:13px">${d==null?'起始':(d>0?'+':'')+d}</span></div>`}).join(''))}
  else h+=grp(`<button class="row center" data-act="addmetric">${ic('chart',18)}添加指标</button>`);
  return h}};
function doCheck(g,amt,dim,inc){const b=gProg(g);const dfrom=dim?Math.min(1,cnt(dim.log,dim.period)/dim.target):null;const o=dim||g;o.log[TODAY]=(o.log[TODAY]||0)+amt;
  if(g.total&&(inc||(inc===undefined&&!dim)))g.total.cur=Math.min(g.total.max,g.total.cur+1);
  const a=gProg(g);if(b.c<b.t&&a.c>=a.t)g.streak++;S._anim={id:g.id,from:b.c/b.t,dim:dim&&dim.id,dfrom};refreshAll();
  hud(dim?`${g.title} · ${dim.title} +1`:(g.unit==='min'?`${g.title} +${amt} 分钟`:`${g.title} +1`))}
ACT.gcheck=t=>{const g=get('g:'+t.dataset.id);if(!g)return;
  if(g.dims.length){actionSheet({title:g.title+' · 打卡',actions:g.dims.map(d=>({label:`${d.title} +1　（${cnt(d.log,d.period)}/${d.target}）`,fn:()=>doCheck(g,1,d)}))});return}
  if(g.unit==='min'){sheet({title:g.title+' · 记录时长',done:'记录',body:`<div class="chips" style="justify-content:center;margin:10px 0 22px">${[15,30,45,60].map(m=>`<button class="chip ${m===30?'on':''}" data-act="chip" data-v="${m}">${m} 分钟</button>`).join('')}</div>${g.total?`<div class="group"><div class="row"><span class="grow">${esc(g.total.name)} +1</span><button class="switch on" data-act="tgllocal" aria-label="${esc(g.total.name)} +1"></button></div></div>`:''}`,
    onDone:w=>{const m=+$('.chip.on',w).dataset.v;const inc=g.total?$('.switch',w).classList.contains('on'):false;doCheck(g,m,null,inc)}});return}
  doCheck(g,1)};
ACT.tgllocal=t=>t.classList.toggle('on');
ACT.dcheck=t=>{const g=tgt();const d=g.dims.find(x=>x.id===t.dataset.d);if(d)doCheck(g,1,d)};
ACT.tot=t=>{const g=tgt();g.total.cur=Math.max(0,Math.min(g.total.max,g.total.cur+(+t.dataset.d)));refreshAll()};
ACT.adddim=()=>{const g=tgt();sheet({title:'添加维度',done:'添加',body:`<div class="group"><input class="fs-in" style="font-size:22px" data-autofocus placeholder="如 游泳" autocomplete="off"></div><div class="seg" style="margin-bottom:16px" data-per>${[['day','每天'],['week','每周'],['month','每月']].map(([v,l])=>`<button class="${v==='week'?'on':''}" data-act="segl" data-v="${v}">${l}</button>`).join('')}</div><div class="group"><div class="row"><span class="grow">目标 <b class="num" data-n>2</b> 次</span><div class="stepper"><button data-act="lstep" data-d="-1" aria-label="减少">${ic('minus',18)}</button><button data-act="lstep" data-d="1" aria-label="增加">${ic('plus',18)}</button></div></div></div>`,
  onDone:w=>{const v=$('.fs-in',w).value.trim();if(!v)return false;g.dims.push({id:nid('d'),title:v,period:$('[data-per] .on',w).dataset.v,unit:'times',target:+$('[data-n]',w).textContent,log:{}});refreshAll();hud('已添加维度')}})};
ACT.lstep=t=>{const n=$('[data-n]',t.closest('.fs-b'));n.textContent=Math.max(1,+n.textContent+(+t.dataset.d))};
ACT.addread=()=>{const g=tgt();const m=g.metric;const last=m.readings[m.readings.length-1].v;sheet({title:'记一次'+m.name,done:'保存',body:`<div class="group"><input class="fs-in mono" data-autofocus inputmode="decimal" value="${last}" aria-label="${esc(m.name)}"></div><div class="sf" style="text-align:center;margin-top:-8px;margin-bottom:12px">${esc(m.unit)} · 今天</div>`,
  onDone:w=>{const v=parseFloat($('.fs-in',w).value);if(!(v>0))return false;const ex=m.readings.find(r=>r.date===TODAY);if(ex)ex.v=v;else m.readings.push({date:TODAY,v});refreshAll();hud('已记录 '+v+' '+m.unit)}})};
ACT.addmetric=()=>{const g=tgt();sheet({title:'添加指标',done:'添加',body:`<div class="group">${[['n','名称','体重',''],['u','单位','kg',''],['s','当前','80','decimal'],['t','目标','75','decimal']].map(([k,l,ph,im],i)=>`<div class="row"><span class="grow">${l}</span><input class="ri" data-k="${k}" placeholder="${ph}" ${im?'inputmode="decimal"':''} ${i===0?'data-autofocus':''} autocomplete="off"></div>`).join('')}</div><div class="fs-err"></div>`,
  onDone:w=>{const v=k=>$(`[data-k=${k}]`,w).value.trim();const s=parseFloat(v('s'));if(!v('n')||!(s>0)){$('.fs-err',w).textContent='填写名称和当前值';return false}g.metric={name:v('n'),unit:v('u'),start:s,target:parseFloat(v('t'))||s,readings:[{date:TODAY,v:s}]};refreshAll();hud('已添加指标')}})};
ACT.goalmore=t=>{const g=tgt();menu(t,[{label:'编辑',icon:'pencil',fn:()=>push('goaledit',{id:g.id})},'-',{label:'删除目标',icon:'trash',danger:true,fn:()=>confirmDel('删除目标？','“'+g.title+'”的打卡记录会一起删除',()=>{S.goals.splice(S.goals.indexOf(g),1);pop();refreshAll();hud('已删除','trash')})}])};
SC.goaledit={title:'编辑目标',tgt:p=>get('g:'+p.id),gone:p=>!get('g:'+p.id),right:()=>`<button class="navbtn b" data-act="back">完成</button>`,
 body(p,s){const o=get('g:'+p.id);return `<div class="sec"><div class="group titlebox"><input class="big-in" data-in="f" data-f="title" value="${esc(o.title)}" placeholder="目标名称" autocomplete="off"></div></div>`+goalForm(o,s,false)}};

/* ================= 投资 ================= */
SC.holding={title:p=>{const h=get('h:'+p.id);return h?h.sym:'标的'},tgt:p=>get('h:'+p.id),gone:p=>!get('h:'+p.id),
 right:()=>`<button class="navbtn" data-act="holdmore" aria-label="更多">${ic('more',24)}</button>`,
 body(p){const h=get('h:'+p.id);const v=h.shares*h.last,c=h.shares*h.avg,pl=v-c,pp=c?pl/c*100:0;const cl=pl>=0?'green':'red';
  let x=`<div class="phead" style="padding-bottom:14px"><div class="muted" style="font-size:15px">${esc(h.name)}</div><h1 style="margin-top:2px">${h.sym}</h1><div style="display:flex;align-items:baseline;gap:8px"><span class="mono" style="font-size:34px;font-weight:600">${h.last.toFixed(2)}</span><span class="muted">USD</span><button class="pill acc" style="margin-left:auto;align-self:center" data-act="holdprice">${ic('pencil',14)}更新价格</button></div></div>`;
  x+=`<div class="sec"><div class="card"><div class="posgrid"><div><div class="k">持仓</div><div class="v mono">${h.shares} 股</div></div><div><div class="k">均价</div><div class="v mono">${h.avg.toFixed(2)}</div></div><div><div class="k">市值</div><div class="v mono">${money(v)}</div></div><div><div class="k">成本</div><div class="v mono">${money(c)}</div></div><div><div class="k">浮动盈亏</div><div class="v mono ${cl}">${money(pl,true)}</div></div><div><div class="k">收益率</div><div class="v mono ${cl}">${pctf(pp,true)}</div></div></div></div></div>`;
  x+=grpH('定投',`<div class="row ir">${tile('repeat','blue')}<span class="grow">每月定投</span><button class="switch ${h.dca.on?'on':''}" data-act="dcatgl" aria-label="定投"></button></div>`+(h.dca.on?`<button class="row ir" data-act="dcaday">${tile('cal','red')}<span class="grow">扣款日</span><span class="val">每月 ${h.dca.day} 号</span>${ic('updown',16,'chev')}</button><button class="row ir" data-act="dcaamt">${tile('money','green')}<span class="grow">金额</span><span class="val mono">${money(h.dca.amount)}</span>${ic('chev',14,'chev')}</button><div class="row ir">${tile('clock','orange')}<span class="grow">下次</span><span class="val">${mdw(nextDca(h))}</span></div>`:''));
  x+=grpH('交易记录',h.trades.map(t=>`<div class="row" style="min-height:56px"><span class="sptag" style="background:${t.side==='buy'?'color-mix(in srgb,var(--ok) 13%,#fff);color:var(--ok)':'color-mix(in srgb,var(--danger) 12%,#fff);color:var(--danger)'}">${t.side==='buy'?'买入':'卖出'}</span><div class="grow"><div class="num" style="font-size:15px">${t.q} 股 × ${t.p.toFixed(2)}</div><div class="t2">${mdw(t.date)}</div></div><span class="mono" style="font-size:15px">${money(t.q*t.p)}</span></div>`).join('')||'<div class="empty">没有交易</div>');
  return x},
 bottom:()=>`<button class="btn okb" data-act="trade" data-side="buy">${ic('plus',18)}记一笔买入</button><button class="btn dangerb" data-act="trade" data-side="sell">卖出</button>`,bcls:()=>'row2'};
ACT.trade=t=>{const h=tgt();sheet({title:'记一笔 · '+h.sym,done:'保存',body:`<div class="seg" style="margin-bottom:16px" data-side>${[['buy','买入'],['sell','卖出']].map(([v,l])=>`<button class="${v===t.dataset.side?'on':''}" data-act="segl" data-v="${v}">${l}</button>`).join('')}</div><div class="group"><div class="row"><span class="grow">股数</span><input class="ri mono" data-k="q" inputmode="decimal" placeholder="0" data-autofocus autocomplete="off"></div><div class="row"><span class="grow">成交价</span><input class="ri mono" data-k="p" inputmode="decimal" value="${h.last}" autocomplete="off"></div><div class="row"><span class="grow">日期</span><span class="val">今天</span></div></div><div class="fs-err"></div>`,
  onDone:w=>{const sd=$('[data-side] .on',w).dataset.v;const q=parseFloat($('[data-k=q]',w).value),pr=parseFloat($('[data-k=p]',w).value);const er=$('.fs-err',w);
    if(!(q>0)||!(pr>0)){er.textContent='填写股数和成交价';return false}if(sd==='sell'&&q>h.shares){er.textContent='最多卖出 '+h.shares+' 股';return false}
    if(sd==='buy'){h.avg=(h.avg*h.shares+q*pr)/(h.shares+q);h.shares+=q}else h.shares-=q;h.shares=Math.round(h.shares*1e4)/1e4;h.avg=Math.round(h.avg*100)/100;
    h.trades.unshift({date:TODAY,side:sd,q,p:pr});refreshAll();hud(sd==='buy'?'已记录买入':'已记录卖出')}})};
ACT.holdprice=()=>{const h=tgt();sheet({title:h.sym+' 最新价',done:'更新',body:`<div class="group"><input class="fs-in mono" data-autofocus inputmode="decimal" value="${h.last}" aria-label="最新价"></div><div class="sf" style="text-align:center;margin-top:-8px">USD</div>`,onDone:w=>{const v=parseFloat($('.fs-in',w).value);if(!(v>0))return false;h.last=v;refreshAll();hud('已更新价格')}})};
ACT.dcatgl=()=>{const h=tgt();h.dca.on=!h.dca.on;refreshAll()};
ACT.dcaday=t=>{const h=tgt();menu(t,[1,5,10,15,20,25].map(d=>({label:'每月 '+d+' 号',checked:h.dca.day===d,fn:()=>{h.dca.day=d;refreshAll()}})))};
ACT.dcaamt=()=>{const h=tgt();sheet({title:'定投金额',done:'保存',body:`<div class="group"><input class="fs-in mono" data-autofocus inputmode="decimal" value="${h.dca.amount}" aria-label="金额"></div><div class="sf" style="text-align:center;margin-top:-8px">USD · 每月 ${h.dca.day} 号</div>`,onDone:w=>{const v=parseFloat($('.fs-in',w).value);if(!(v>0))return false;h.dca.amount=v;refreshAll();hud('已更新定投金额')}})};
ACT.holdmore=t=>{const h=tgt();menu(t,[{label:'删除标的',icon:'trash',danger:true,fn:()=>confirmDel('删除 '+h.sym+'？','交易记录会一起删除',()=>{S.holdings.splice(S.holdings.indexOf(h),1);pop();refreshAll();hud('已删除','trash')})}])};
const ALC=['var(--accent)','#0F8B8D','#C2410C','#1B8A55','#8E8E93'];
SC.invest={title:'投资',large:true,right:()=>`<button class="navbtn" data-act="addhold" aria-label="添加标的">${ic('plus',24)}</button>`,
 body(){const t=holdTot();
  let h=`<div class="sec">${invSummary(false)}</div>`;
  if(t.v>0)h+=`<div class="sec"><div class="sh">配置</div><div class="card" style="padding:14px 16px"><div style="display:flex;height:10px;border-radius:5px;overflow:hidden;gap:2px">${S.holdings.map((x,i)=>`<i style="flex:${x.shares*x.last};background:${ALC[i%5]}"></i>`).join('')}</div><div style="display:flex;flex-wrap:wrap;gap:6px 16px;margin-top:10px;font-size:13px">${S.holdings.map((x,i)=>`<span style="display:inline-flex;align-items:center;gap:6px"><i class="dot" style="background:${ALC[i%5]}"></i>${x.sym} <span class="muted num">${(x.shares*x.last/t.v*100).toFixed(1)}%</span></span>`).join('')}</div></div></div>`;
  h+=grpH('持仓',S.holdings.map(holdRow).join('')||'<div class="empty">没有持仓</div>');
  h+=grpH('定投计划',S.holdings.map(x=>`<button class="row" data-act="open" data-ref="h:${x.id}"><span class="grow">${x.sym}</span><span class="val">${x.dca.on?`每月 ${x.dca.day} 号 · <span class="mono">${money(x.dca.amount).replace('.00','')}</span>`:'已暂停'}</span>${ic('chev',14,'chev')}</button>`).join(''));
  return h}};
ACT.addhold=()=>sheet({title:'添加标的',done:'添加',body:`<div class="group">${[['s','代码','如 SCHD',''],['q','股数','0','decimal'],['a','均价','0.00','decimal'],['l','最新价','0.00','decimal']].map(([k,l,ph,im],i)=>`<div class="row"><span class="grow">${l}</span><input class="ri ${im?'mono':''}" data-k="${k}" placeholder="${ph}" ${im?'inputmode="decimal"':'autocapitalize="characters"'} ${i===0?'data-autofocus':''} autocomplete="off"></div>`).join('')}</div><div class="fs-err"></div>`,
  onDone:w=>{const v=k=>$(`[data-k=${k}]`,w).value.trim();const sym=v('s').toUpperCase(),q=parseFloat(v('q'))||0,a=parseFloat(v('a'))||0,l=parseFloat(v('l'))||a;if(!sym){$('.fs-err',w).textContent='填写代码';return false}
    S.holdings.push({id:nid('h'),sym,name:sym,shares:q,avg:a,last:l,dca:{on:false,day:15,amount:500},trades:q?[{date:TODAY,side:'buy',q,p:a}]:[]});refreshAll();hud('已添加 '+sym)}});

/* ================= 列表页 ================= */
SC.projects={title:'项目',large:true,right:()=>`<button class="navbtn" data-act="newk" data-k="project" aria-label="新建项目">${ic('plus',24)}</button>`,
 body(){const xs=S.projects.filter(p=>(S.projTab==='archived')===p.archived&&spOK(p));
  return `<div class="newseg">${seg([['active','进行中'],['archived','已归档']],S.projTab,'projtab')}</div><div class="sec cards plist">${xs.map(projCard).join('')||'<div class="card empty">没有项目</div>'}</div>`}};
ACT.projtab=t=>{S.projTab=t.dataset.v;render(cur())};
ACT.newk=t=>push('new',{kind:t.dataset.k});
SC.goals={title:'目标',large:true,right:()=>`<button class="navbtn" data-act="newk" data-k="goal" aria-label="新建目标">${ic('plus',24)}</button>`,
 body(){const xs=S.goals.filter(spOK);return grp(xs.map(goalRow).join('')||'<div class="empty">没有目标</div>')}};

/* ================= 设置 ================= */
function navRow(icn,c,label,val,to){return `<button class="row ir" data-act="push" data-to="${to}">${tile(icn,c)}<span class="grow">${label}</span>${val?`<span class="val">${val}</span>`:''}${ic('chev',14,'chev')}</button>`}
const ACCENTS=[['#1F5BFF','蓝色'],['#C2410C','陶土'],['#1B8A55','松绿'],['#0F8B8D','青色'],['#3A3A3C','石墨']];
SC.settings={title:'设置',large:true,
 body(){return grp(`<div class="row" style="padding:12px 16px;gap:14px"><span class="av" style="width:58px;height:58px;font-size:21px">LL</span><div class="grow"><div style="font-size:20px;font-weight:600">Liang Li</div><div class="t2">Apple ID 登录 · iCloud 同步已开启</div></div></div>`)
  +grp(navRow('sparkle','blue','接入 AI 助手',S.ai.list.length+' 个令牌','ai'))
  +grp(navRow('palette','orange','主题',ACCENTS.find(a=>a[0]===S.accent)[1],'theme')+`<div class="row ir">${tile('bellx','gray')}<span class="grow">提醒静音</span><button class="switch ${S.mute.on?'on':''}" data-act="mute" aria-label="提醒静音"></button></div>`+(S.mute.on?`<button class="row ir" data-act="muterange">${tile('clock','graphite')}<span class="grow">静音时段</span><span class="val num">${S.mute.range}</span>${ic('updown',16,'chev')}</button>`:'')+navRow('export','green','导出数据','','export'))
  +grp(navRow('star','rust','Noted Pro',S.pro?'已开通':'','pro'))
  +`<div class="sf" style="text-align:center;padding-bottom:20px">Noted 2.0 · 设计原型</div>`}};
ACT.mute=()=>{S.mute.on=!S.mute.on;render(cur());hud(S.mute.on?'已开启提醒静音':'已关闭提醒静音',S.mute.on?'bellx':'bell')};
ACT.muterange=t=>menu(t,['22:00 – 07:30','23:00 – 08:00','00:00 – 09:00'].map(r=>({label:r,checked:S.mute.range===r,fn:()=>{S.mute.range=r;render(cur())}})));
const MASK=t=>t.slice(0,6)+'••••••••••••'+t.slice(-4);
function aiTexts(){const k=S.ai.cur;const real=k?k.tok:'<令牌>';const shown=k&&S.ai.show?k.tok:(k?MASK(k.tok):'<令牌>');
  const cc=x=>`claude mcp add --transport http noted https://noted.superleo.cn/mcp --header "Authorization: Bearer ${x}"`;
  const cx=`# ~/.codex/config.toml\n[mcp_servers.noted]\nurl = "https://noted.superleo.cn/mcp"\nbearer_token_env_var = "NOTED_TOKEN"`;
  const ex=x=>`export NOTED_TOKEN="${x}"`;
  return{disp:{tok:shown,cc:cc(shown),cx,ex:ex(shown)},real:{tok:real,cc:cc(real),cx,ex:ex(real)}}}
const codeBlock=(text,k)=>`<div class="code" data-code="${k}">${esc(text)}<button class="cp" data-act="copy" data-k="${k}" aria-label="复制">${ic('copy',18)}</button></div>`;
SC.ai={title:'接入 AI 助手',
 body(){const k=S.ai.cur;const tx=aiTexts();
  let h=k?grpH('访问令牌',`<div style="padding:14px 16px"><div class="tokbox"><span>${esc(tx.disp.tok)}</span><button class="navbtn" style="height:34px;min-width:34px;padding:0" data-act="tokshow" aria-label="显示令牌">${ic('eye',20)}</button></div><div style="display:flex;gap:10px;margin-top:12px"><button class="btn tint sm" data-act="copy" data-k="tok">${ic('copy',18)}复制</button><button class="btn ghost sm" data-act="tokgen">${ic('key',18)}生成新令牌</button></div></div>`,'',`${esc(k.name)} · ${md(k.created)} 创建`)
    :grp(`<div style="padding:16px"><button class="btn" data-act="tokgen">${ic('key',18)}生成令牌</button></div>`);
  h+=`<div class="sec"><div class="sh">Claude Code · 终端运行</div>${codeBlock(tx.disp.cc,'cc')}</div>`;
  h+=`<div class="sec"><div class="sh">Codex · 写入配置</div>${codeBlock(tx.disp.cx,'cx')}<div style="height:10px"></div>${codeBlock(tx.disp.ex,'ex')}</div>`;
  h+=grpH('已签发的令牌',S.ai.list.map(x=>`<div class="row" style="min-height:58px"><div class="grow"><div class="t1">${esc(x.name)}${x===S.ai.cur?' <span class="sptag work" style="height:20px;font-size:11px;vertical-align:2px">当前</span>':''}</div><div class="t2">${md(x.created)} 创建 · 最近使用 ${esc(x.used)}</div></div><button class="pill" style="color:var(--danger)" data-act="tokrevoke" data-id="${x.id}">吊销</button></div>`).join('')||'<div class="empty">没有令牌</div>');
  return h}};
ACT.tokshow=()=>{S.ai.show=!S.ai.show;render(cur())};
ACT.tokgen=()=>sheet({title:'生成令牌',done:'生成',body:`<div class="group"><input class="fs-in" style="font-size:22px" data-autofocus value="Claude Code" aria-label="令牌名称" autocomplete="off"></div><div class="sf" style="text-align:center;margin-top:-8px">名称</div>`,
  onDone:w=>{const n=$('.fs-in',w).value.trim()||'新令牌';const k={id:nid('k'),name:n,tok:genTok(),created:TODAY,used:'未使用'};S.ai.list.unshift(k);S.ai.cur=k;S.ai.show=true;render(cur());hud('已生成令牌','key')}});
ACT.tokrevoke=t=>{const k=S.ai.list.find(x=>x.id===t.dataset.id);confirmDel('吊销“'+k.name+'”？','使用它的客户端会立即断开',()=>{S.ai.list.splice(S.ai.list.indexOf(k),1);if(S.ai.cur===k){S.ai.cur=S.ai.list[0]||null;S.ai.show=false}refreshAll();hud('已吊销','key')},'吊销')};
ACT.copy=t=>{const k=t.dataset.k;const text=aiTexts().real[k];const el=t.closest('.code')||t.closest('.sec');
  const ok=()=>hud('已复制','copy');
  const fallback=()=>{const ta=document.createElement('textarea');ta.value=text;ta.setAttribute('readonly','');ta.style.cssText='position:fixed;top:0;left:0;opacity:0;font-size:16px';document.body.appendChild(ta);ta.focus();ta.select();ta.setSelectionRange(0,text.length);let r=false;try{r=document.execCommand('copy')}catch(e){}ta.remove();
    if(r)return ok();const node=el.querySelector('.code')||el;const rg=document.createRange();rg.selectNodeContents(node);const sl=getSelection();sl.removeAllRanges();sl.addRange(rg);hud('已选中，长按复制','copy')};
  if(navigator.clipboard&&window.isSecureContext)navigator.clipboard.writeText(text).then(ok,fallback);else fallback()};
SC.theme={title:'主题',body(){return grpH('强调色',ACCENTS.map(([c,l])=>`<button class="row" data-act="accent" data-v="${c}"><i class="dot" style="width:22px;height:22px;background:${c}"></i><span class="grow">${l}</span>${S.accent===c?`<span style="color:var(--accent)">${ic('check',20)}</span>`:''}</button>`).join(''))+grpH('预览',`<div style="padding:16px;display:flex;gap:10px;align-items:center"><button class="btn sm" style="flex:1" data-act="accent" data-v="${S.accent}">按钮</button><span class="ck on">${ic('check',14)}</span><button class="switch on" data-act="tgllocal" aria-label="示例开关"></button></div>`)}};
ACT.accent=t=>{S.accent=t.dataset.v;document.documentElement.style.setProperty('--accent',S.accent);refreshAll()};
SC.export={title:'导出数据',init(p,s){s.st.f='md';s.st.r='all'},
 body(p,s){return grpH('格式',[['md','Markdown','笔记为 .md，待办与日程为清单'],['json','JSON','完整数据，可再导入'],['ics','日历 (.ics)','日程与带日期的待办']].map(([k,l,d])=>`<button class="row" data-act="expf" data-v="${k}" style="min-height:58px"><div class="grow"><div class="t1">${l}</div><div class="t2">${d}</div></div>${s.st.f===k?`<span style="color:var(--accent)">${ic('check',20)}</span>`:''}</button>`).join(''))
   +grpH('范围',`<div class="row">${seg([['all','全部'],['personal','个人'],['work','工作']],s.st.r,'expr')}</div>`.replace('class="seg"','class="seg" style="flex:1"'))
   +`<div class="sec"><button class="btn" data-act="expdo">${ic('export',20)}导出</button></div>`}};
ACT.expf=t=>{cur().st.f=t.dataset.v;render(cur())};
ACT.expr=t=>{cur().st.r=t.dataset.v;render(cur())};
ACT.expdo=()=>{const s=cur();hud('已导出 noted-20261010.'+(s.st.f==='md'?'zip':s.st.f),'export')};
SC.pro={title:'Noted Pro',init(p,s){s.st.plan='y'},
 body(p,s){return `<div class="prohero"><span class="tile" style="background:var(--t-rust)">${ic('star',34)}</span><h1>Noted Pro</h1><div class="muted">${S.pro?'已开通，感谢支持':'一次订阅，所有设备'}</div></div>`
  +grp(['多设备 iCloud 同步','接入 AI 助手（MCP）','无限项目与目标','投资与定投追踪','导出全部数据'].map(x=>`<div class="row"><span style="color:var(--ok)">${ic('check',20)}</span><span class="grow">${x}</span></div>`).join(''))
  +(S.pro?'':`<div class="sec" style="display:flex;gap:10px"><button class="price ${s.st.plan==='m'?'on':''}" data-act="plan" data-v="m"><span>月付</span><b class="num">¥12</b><span>每月</span></button><button class="price ${s.st.plan==='y'?'on':''}" data-act="plan" data-v="y"><span>年付 · 省 32%</span><b class="num">¥98</b><span>每年</span></button></div><div class="sec"><button class="btn" data-act="buypro">订阅</button></div>`)}};
ACT.plan=t=>{cur().st.plan=t.dataset.v;render(cur())};
ACT.buypro=()=>{S.pro=true;refreshAll();hud('已开通 Pro','star')};

/* ================= root actions & boot ================= */
ACT.tab=t=>setTab(t.dataset.tab);
ACT.new=()=>push('new',{});
ACT.push=t=>push(t.dataset.to,{});
ACT.open=t=>openRef(t.dataset.ref);
ACT.back=()=>pop();
TABS.forEach(t=>{const s=mk(t,{},t);stacks[t].push(s);tvs[t].appendChild(s.el)});
tvs.today.classList.add('on');
TABS.forEach(t=>render(stacks[t][0]));
fit();
</script>
