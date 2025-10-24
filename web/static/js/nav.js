(function(){
  document.addEventListener('DOMContentLoaded', function(){
    var links = [
      { path: '/admin/collector', label: '采集器主页', emoji: '📊' },
      { path: '/admin/device-types', label: '设备类型', emoji: '🧩' },
      { path: '/admin/devices', label: '设备清单', emoji: '📋' },
      { path: '/admin/ssh-adapter', label: 'SSH适配', emoji: '🔧' },
      { path: '/admin/logs', label: '日志查询', emoji: '🗂️' },
      { path: '/admin/quick-collect', label: '快速采集', emoji: '⚡' },
      { path: '/admin/simulate', label: '模拟设置', emoji: '🎛️' },
      { path: '/admin/simulate-data', label: '模拟数据', emoji: '🧪' }
    ];

    var nav = document.createElement('nav');
    nav.className = 'sidebar';
    var html = '<div class="brand">SSH Collector</div>';
    html += '<div class="menu">';
    html += '<small>管理导航</small>';
    for (var i=0; i<links.length; i++) {
      var l = links[i];
      html += '<a href="'+ l.path +'" data-path="'+ l.path +'">' + (l.emoji||'') + ' ' + l.label + '</a>';
    }
    html += '</div>';
    nav.innerHTML = html;

    document.body.appendChild(nav);
    document.body.classList.add('has-sidebar');

    var current = location.pathname.replace(/\/+$/, '');
    var as = nav.querySelectorAll('a');
    for (var j=0; j<as.length; j++) {
      var a = as[j];
      var p = (a.getAttribute('data-path')||'').replace(/\/+$/, '');
      if (p === current) a.classList.add('active');
    }
  });
})();