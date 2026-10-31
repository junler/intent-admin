import '@/styles/global.css';
import '@/styles/markdown.css';

import dayjs from 'dayjs';
import 'dayjs/locale/zh-cn';
import { getLocale } from '@/i18n';

document.documentElement.lang = getLocale();
dayjs.locale(getLocale() === 'en-US' ? 'en' : 'zh-cn');
