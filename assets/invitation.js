document.addEventListener('DOMContentLoaded', () => {
    const start = document.getElementById('Start');
    if (start && !start.value) {
        const date = new Date();
        date.setHours(date.getHours() + 1, 0, 0, 0);
        const local = new Date(date.getTime() - date.getTimezoneOffset() * 60000);
        start.value = local.toISOString().slice(0, 16);
    }
    const timezone = document.getElementById('TimeZone');
    if (timezone && !timezone.value) {
        timezone.value = Intl.DateTimeFormat().resolvedOptions().timeZone;
    }
    document.querySelectorAll('form[data-progress]').forEach(form => {
        form.addEventListener('submit', event => {
            if (form.dataset.submitting === 'true') {
                event.preventDefault();
                return;
            }
            form.dataset.submitting = 'true';
            const message = form.querySelector('[data-progress-message]');
            message.textContent = form.dataset.progress;
            message.hidden = false;
            // Wait for form serialization so the clicked button's name/value
            // (Generate or Action) is included in the request.
            setTimeout(() => form.querySelectorAll('button').forEach(button => { button.disabled = true; }), 0);
        });
    });
});

// Restore controls when the browser returns to a form from its page cache.
window.addEventListener('pageshow', () => {
    document.querySelectorAll('form[data-progress]').forEach(form => {
        delete form.dataset.submitting;
        form.querySelectorAll('button').forEach(button => { button.disabled = false; });
        form.querySelector('[data-progress-message]').hidden = true;
    });
});
