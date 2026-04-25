var copyStatus = document.getElementById('copy-status');
document.querySelectorAll('.copy-btn').forEach(function(btn) {
  btn.addEventListener('click', function() {
    var code = btn.parentElement.querySelector('code').textContent;
    navigator.clipboard.writeText(code).then(function() {
      btn.textContent = 'Copied!';
      btn.classList.add('copied');
      copyStatus.textContent = 'Copied to clipboard';
      setTimeout(function() {
        btn.textContent = 'Copy';
        btn.classList.remove('copied');
        copyStatus.textContent = '';
      }, 1500);
    }).catch(function() {
      btn.textContent = 'Failed';
      setTimeout(function() { btn.textContent = 'Copy'; }, 1500);
    });
  });
});
