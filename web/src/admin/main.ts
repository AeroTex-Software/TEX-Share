import '../app.css';
import App from './App.svelte';
import { configureAPI } from '../lib/api';
import { startRouter } from '../lib/router';
import { installUnauthorizedHandler } from '../lib/stores';

configureAPI('admin');
installUnauthorizedHandler('/login');
startRouter();

const target = document.getElementById('app');
if (target) {
  new App({ target });
}
