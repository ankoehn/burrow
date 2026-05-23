// Burrow OpenAPI Viewer — hand-rolled, zero external dependencies.
// Single same-origin GET via fetch: /api/v1/openapi.yaml. No CDN, no phone-home.
'use strict';

// ── Theme toggle ──────────────────────────────────────────────────────────────
(function(){
  var btn=document.getElementById('theme-toggle'), body=document.body;
  var s=localStorage.getItem('burrow-theme');
  if(s) body.classList.add(s);
  btn.onclick=function(){
    var dark=body.classList.contains('dark');
    body.classList.replace(dark?'dark':'light', dark?'light':'dark') ||
      body.classList.add(dark?'light':'dark');
    localStorage.setItem('burrow-theme', dark?'light':'dark');
  };
})();

// ── Escape HTML ───────────────────────────────────────────────────────────────
function esc(s){ return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;'); }

// ── Tiny OpenAPI v3 YAML recogniser ──────────────────────────────────────────
// Strategy: walk by indentation. paths: at col 0, /path: at col 2,
// method: at col 4, sub-keys at col 6+. Does not attempt full YAML parsing.
function parseOpenAPI(text){
  var lines=text.split('\n'), doc={info:{version:''},paths:{}};
  var inPaths=false, path=null, meth=null, ctx=null, param=null;
  var METHODS=['get','post','put','patch','delete','head','options'];

  function ind(l){ var m=l.match(/^(\s*)/); return m?m[1].length:0; }
  function kv(l){ var m=l.match(/^\s*([\w.\-\/{}]+)\s*:\s*(.*)/); return m?[m[1],m[2].trim()]:null; }
  function str(v){ return v.replace(/^[>|]\s*/,'').replace(/^['"]/,'').replace(/['"]$/,''); }

  var PREF={ PathID:{name:'id',in:'path',required:true,schema:'string'},
             PathServiceID:{name:'serviceID',in:'path',required:true,schema:'string'} };

  for(var i=0;i<lines.length;i++){
    var l=lines[i];
    if(!l.trim()||l.trim()[0]==='#') continue;
    var d=ind(l), p=kv(l);

    if(d===0){
      inPaths = p&&p[0]==='paths';
      if(p&&p[0]==='info') doc.info={version:''};
      path=meth=ctx=param=null; continue;
    }
    if(!inPaths){
      if(d===2&&p&&p[0]==='version') doc.info.version=str(p[1]);
      continue;
    }

    if(d===2&&p&&p[0][0]==='/'){
      path=p[0]; meth=ctx=param=null;
      if(!doc.paths[path]) doc.paths[path]={};
      continue;
    }
    if(!path) continue;

    if(d===4&&p&&METHODS.indexOf(p[0])!==-1){
      meth=p[0].toUpperCase(); ctx=param=null;
      doc.paths[path][meth]={summary:'',parameters:[],hasBody:false,responses:{}};
      continue;
    }
    if(!meth) continue;
    var e=doc.paths[path][meth];

    if(d===6&&p){
      ctx=null; param=null;
      if(p[0]==='summary'){ e.summary=str(p[1]); continue; }
      if(p[0]==='tags'){
        var tm=p[1].match(/\[([^\]]*)\]/);
        e.tags=tm?tm[1].split(',').map(function(t){return t.trim();}):[];
        ctx='tags'; continue;
      }
      if(p[0]==='parameters'){ ctx='params'; continue; }
      if(p[0]==='requestBody'){ e.hasBody=true; ctx='body'; continue; }
      if(p[0]==='responses'){ ctx='resp'; continue; }
      continue;
    }

    if(ctx==='tags'&&d===8){ var t=l.trim(); if(t[0]==='-') e.tags.push(t.slice(1).trim()); continue; }

    if(ctx==='params'){
      if(d===8){
        var rm=l.match(/\$ref:\s*['"]?#\/components\/parameters\/(\w+)/);
        if(rm){ e.parameters.push(PREF[rm[1]]||{name:rm[1],in:'path',required:true,schema:'string'}); param=null; }
        else{ var nm=l.match(/name\s*:\s*(.+)/); if(nm){ param={name:nm[1].trim(),in:'',required:false,schema:''}; e.parameters.push(param); } }
        continue;
      }
      if(param&&d===10&&p){ if(p[0]==='in') param.in=p[1]; else if(p[0]==='required') param.required=p[1]==='true'; continue; }
      if(param&&d===12&&p&&p[0]==='type'){ param.schema=p[1]; continue; }
    }

    if(ctx==='resp'&&d===8&&p){
      var rv=p[1], rfm=rv.match(/\$ref:\s*['"]?#\/components\/responses\/(\w+)/);
      var KR={Unauthorized:'Unauthorized',Forbidden:'Forbidden',NotFound:'Not Found',BadRequest:'Bad Request'};
      e.responses[p[0]]=rfm?(p[0]+' – '+(KR[rfm[1]]||rfm[1])):'';
      continue;
    }
    if(ctx==='resp'&&d===10&&p&&p[0]==='description'){
      var codes=Object.keys(e.responses);
      if(codes.length&&!e.responses[codes[codes.length-1]]) e.responses[codes[codes.length-1]]=str(p[1]);
      continue;
    }
  }
  return doc;
}

// ── State ─────────────────────────────────────────────────────────────────────
var allRoutes=[], activeEl=null, currentAnchor='';

function anc(path,meth){ return encodeURIComponent(meth+' '+path); }

function mclass(m){ return 'method method-'+m; }

function rclass(code){
  var n=parseInt(code,10);
  return 'resp-code'+(n<300?' resp-2xx':n<500?' resp-4xx':n>=500?' resp-5xx':'');
}

// ── Render detail ─────────────────────────────────────────────────────────────
function renderDetail(path,meth,e){
  var d=document.getElementById('detail');
  var ph='', bh='', rh='';

  if(e.parameters&&e.parameters.length){
    var rows=e.parameters.map(function(p){
      return '<tr><td>'+esc(p.name)+(p.required?' <span class="param-required">*</span>':'')+
        '</td><td><span class="param-in">'+esc(p.in||'')+'</span></td>'+
        '<td>'+esc(p.schema||'')+'</td></tr>';
    }).join('');
    ph='<div class="section"><div class="section-title">Parameters</div>'+
      '<table class="param-table"><thead><tr><th>Name</th><th>In</th><th>Type</th></tr></thead>'+
      '<tbody>'+rows+'</tbody></table></div>';
  }
  if(e.hasBody) bh='<div class="section"><div class="section-title">Request Body</div><p>JSON body required.</p></div>';

  var codes=Object.keys(e.responses||{});
  if(codes.length){
    var items=codes.map(function(c){
      return '<li class="resp-item"><span class="'+rclass(c)+'">'+esc(c)+'</span>'+
        '<span class="resp-desc">'+esc(e.responses[c])+'</span></li>';
    }).join('');
    rh='<div class="section"><div class="section-title">Responses</div><ul class="resp-list">'+items+'</ul></div>';
  }

  d.innerHTML='<div class="detail-header">'+
    '<div><span class="'+mclass(meth)+'">'+esc(meth)+'</span> <span class="detail-path">'+esc(path)+'</span></div>'+
    (e.summary?'<p class="detail-summary">'+esc(e.summary)+'</p>':'')+
    '</div>'+ph+bh+rh;
}

// ── Sidebar ───────────────────────────────────────────────────────────────────
function buildSidebar(filter){
  var list=document.getElementById('route-list'), lf=filter?filter.toLowerCase():'';
  list.innerHTML='';
  allRoutes.forEach(function(r){
    if(lf&&r.path.toLowerCase().indexOf(lf)===-1&&r.method.toLowerCase().indexOf(lf)===-1) return;
    var li=document.createElement('li'), btn=document.createElement('button');
    var a=anc(r.path,r.method);
    btn.className='route-item'+(a===currentAnchor?' active':'');
    btn.setAttribute('data-anchor',a);
    btn.innerHTML='<span class="'+mclass(r.method)+'">'+esc(r.method)+'</span>'+
      '<span class="route-path">'+esc(r.path)+'</span>';
    btn.onclick=function(){ selectRoute(r.path,r.method,btn); };
    if(a===currentAnchor) activeEl=btn;
    li.appendChild(btn); list.appendChild(li);
  });
}

function selectRoute(path,meth,el){
  if(activeEl) activeEl.classList.remove('active');
  activeEl=el; if(el) el.classList.add('active');
  var a=anc(path,meth);
  if(window.location.hash!=='#'+a) history.replaceState(null,'','#'+a);
  currentAnchor=a;
  for(var i=0;i<allRoutes.length;i++){
    if(allRoutes[i].path===path&&allRoutes[i].method===meth){
      renderDetail(path,meth,allRoutes[i].entry); return;
    }
  }
}

// ── Init ──────────────────────────────────────────────────────────────────────
function init(doc){
  var ORDER=['GET','POST','PUT','PATCH','DELETE'];
  Object.keys(doc.paths).sort().forEach(function(p){
    var ms=doc.paths[p];
    ORDER.forEach(function(m){ if(ms[m]) allRoutes.push({path:p,method:m,entry:ms[m]}); });
    Object.keys(ms).forEach(function(m){ if(ORDER.indexOf(m)===-1) allRoutes.push({path:p,method:m,entry:ms[m]}); });
  });

  buildSidebar('');

  var hash=window.location.hash.slice(1);
  if(hash){
    currentAnchor=hash;
    var dec=decodeURIComponent(hash), sp=dec.indexOf(' ');
    if(sp>0){
      var hm=dec.slice(0,sp), hp=dec.slice(sp+1);
      var el=document.querySelector('[data-anchor="'+hash+'"]');
      if(el){ el.classList.add('active'); activeEl=el; el.scrollIntoView({block:'nearest'}); }
      for(var i=0;i<allRoutes.length;i++){
        if(allRoutes[i].path===hp&&allRoutes[i].method===hm){ renderDetail(hp,hm,allRoutes[i].entry); break; }
      }
    }
  }

  document.getElementById('search').addEventListener('input',function(e){
    buildSidebar(e.target.value);
    if(currentAnchor){
      var el=document.querySelector('[data-anchor="'+currentAnchor+'"]');
      if(el){ el.classList.add('active'); activeEl=el; }
    }
  });
}

// ── Single same-origin GET ────────────────────────────────────────────────────
fetch('/api/v1/openapi.yaml')
  .then(function(r){ if(!r.ok) throw new Error('HTTP '+r.status); return r.text(); })
  .then(function(text){ init(parseOpenAPI(text)); })
  .catch(function(err){
    document.getElementById('route-list').innerHTML=
      '<li style="padding:0.5rem;color:#dc2626">Failed to load spec: '+esc(String(err))+'</li>';
  });
