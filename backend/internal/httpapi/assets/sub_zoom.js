(function(){
  var wrap=document.querySelector('.map-wrap');
  if(!wrap) return;
  var inner=wrap.querySelector('.map-inner');
  var panel=wrap.querySelector('.map-panel');
  var reset=wrap.querySelector('.map-reset');
  if(!inner) return;
  var markers=[].slice.call(wrap.querySelectorAll('.marker'));
  var W=1000,H=500,ZOOM=3.8,RADIUS=75;
  function esc(s){var d=document.createElement('div');d.textContent=(s==null?'':s);return d.innerHTML;}
  function data(){return markers.map(function(m){return {
    x:+m.getAttribute('data-x'),y:+m.getAttribute('data-y'),ip:m.getAttribute('data-ip'),
    city:m.getAttribute('data-city'),flag:m.getAttribute('data-flag'),hits:m.getAttribute('data-hits')};});}
  function fillPanel(list){
    if(!panel) return;
    if(!list.length){panel.className='map-panel';panel.innerHTML='';return;}
    var h='<div class="mp-h">\u{1F465} کاربران این منطقه ('+list.length+')</div>';
    list.forEach(function(c){h+='<div class="mp-row"><img src="'+esc(c.flag)+'" alt="">'+
      '<span class="mp-ip">'+esc(c.ip)+'</span><span class="mp-city">'+esc(c.city)+
      '</span><span class="mp-h2">'+esc(c.hits)+'×</span></div>';});
    panel.innerHTML=h;panel.className='map-panel show';
  }
  function zoomTo(x,y,instant){
    if(instant) inner.style.transition='none';
    inner.style.transformOrigin=(x/W*100)+'% '+(y/H*100)+'%';
    inner.style.transform='scale('+ZOOM+')';
    wrap.classList.add('zoomed');
    fillPanel(data().filter(function(c){var dx=c.x-x,dy=c.y-y;return Math.sqrt(dx*dx+dy*dy)<=RADIUS;}));
    if(instant){void inner.offsetWidth; inner.style.transition='';}
    try{history.replaceState(null,'','#z='+Math.round(x)+','+Math.round(y));}catch(e){}
  }
  function zoomOut(){
    inner.style.transform='';wrap.classList.remove('zoomed');
    if(panel){panel.className='map-panel';panel.innerHTML='';}
    try{history.replaceState(null,'',location.pathname+location.search);}catch(e){}
  }
  markers.forEach(function(m){m.addEventListener('click',function(e){
    e.preventDefault();e.stopPropagation();
    zoomTo(+m.getAttribute('data-x'),+m.getAttribute('data-y'));});});
  inner.addEventListener('click',function(e){
    if(wrap.classList.contains('zoomed'))return;
    var r=inner.getBoundingClientRect();
    zoomTo((e.clientX-r.left)/r.width*W,(e.clientY-r.top)/r.height*H);});
  if(reset) reset.addEventListener('click',zoomOut);
  var mm=/#z=(\d+),(\d+)/.exec(location.hash);
  if(mm) zoomTo(+mm[1],+mm[2],true);
})();
