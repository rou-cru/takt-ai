// Contenido real del Home completo y el flujo de instalación (takt/tui/tui.go,
// takt/tui/install/install.go). No son fixtures genéricos: son las pantallas
// reales, con sus variantes cuando el flujo se ramifica por una decisión previa
// (Setup Default/Custom, Review Default/Custom, Result éxito/cancelado/error).
export const screens = [];
const t = (x,y,text,role='text') => ({x,y,text,role});
const p = (x,y,w,h,label='',role='panel') => ({x,y,w,h,label,role});
function add(id,title,pattern,text,panels=[],options={}) {
  screens.push({id,title,pattern,width:120,height:32,text,panels,...options});
}
const shell = (title,width=120) => [t(3,1,'Takt AI','brand'), t(width-3-title.length,1,title,'heading')];
const button = (x,y,label,active=false) => t(x,y,`  ${label}  `,active?'btn-focus':'btn');
const bar = (x,y) => t(x,y,'┃','bar');

add('01-home','Home / menú completo','L03',[
  t(65,12,'> Configure installation','focus'),
  t(65,13,'Assign models'),
  t(65,14,'Check for drift'),
  t(65,15,'Uninstall'),
  t(65,16,'Diagnostics'),
  t(65,17,'Quit'),
  t(42,23,'Takt AI','brand'),
],[],
{logo:{x:29,y:7,w:34,h:16}});

add('03-home-narrow','Home / terminal estrecho','L03 adaptado',[
  t(26,9,'Takt AI','brand'),
  t(17,11,'> Configure installation','focus'),
  t(17,12,'Assign models'),
  t(17,13,'Check for drift'),
  t(17,14,'Uninstall'),
  t(17,15,'Diagnostics'),
  t(17,16,'Quit'),
],[],
{width:60,height:20,logo:{x:24,y:3,w:12,h:6}});

add('04-setup-default','Install · Setup — Default enfocado','L05 + selector',[
  ...shell('Setup'),
  bar(37,15), bar(37,16),
  t(39,15,'> Default','focus'),
  t(41,16,'install the recommended stack','muted'),
  t(41,18,'Custom'),
  t(41,19,'choose what to include','muted'),
],[p(35,13,50,9)]);

add('15-setup-custom','Install · Setup — Custom enfocado','L05 + selector',[
  t(3,1,'Takt AI','brand'),t(72,1,'Setup','heading'),
  t(21,11,'Default'),
  t(21,12,'install the recommended stack','muted'),
  bar(17,14), bar(17,15),
  t(19,14,'> Custom','focus'),
  t(21,15,'choose what to include','muted'),
],[p(15,9,50,9)],
{width:80,height:24});

add('05-components','Install · Components','L04',[
  ...shell('Components'),
  t(23,13,'Choose what to include. Takt agents and core skills are always installed.','muted'),
  bar(23,15), bar(23,16),
  t(25,15,'> [x]','muted'), t(31,15,'Context7','checked'),
  t(31,16,'up-to-date library docs for agents','muted'),
  t(25,18,'  [ ]','muted'), t(31,18,'Takt theme'),
  t(31,19,'terminal theme for the harness','muted'),
  t(25,21,'  [x]','muted'), t(31,21,'Sub-agent status line','checked'),
],[p(21,11,78,13)]);

add('06-review','Install · Review — Default','L05',[
  ...shell('Review'),
  t(36,13,'Scope','muted'), t(49,13,'OpenCode'),
  t(36,15,'Components','muted'), t(49,15,'Default (all recommended)'),
  t(36,16,'Add','muted'), t(49,16,'16 new files'),
  t(49,17,'  4 agent definitions'),
  t(49,18,'  1 skill'),
  button(45,22,'Personalize'), button(62,22,'Install',true),
],[p(34,11,52,10)]);

add('07-review-narrow','Install · Review — Custom','L05 adaptado',[
  t(2,1,'Takt AI','brand'),t(52,1,'Review','heading'),
  t(7,9,'Scope','muted'), t(20,9,'OpenCode'),
  t(7,10,'Components','muted'), t(20,10,'Context7'),
  t(20,11,'Sub-agent status line'),
  button(15,15,'Personalize'), button(32,15,'Install',true),
],[p(5,7,50,7)],{width:60,height:20});

add('08-progress','Install · Installing','L08',[
  ...shell('Installing'),
  t(38,14,'████████████░░░░░░░░░░░░░░░░  40%'),
  t(38,16,'✓ Checking existing files','checked'),
  t(38,17,'✓ Installing agent definitions','checked'),
  t(38,18,'⠋ Installing skills','heading'),
  t(38,19,'  Configuring OpenCode','muted'),
  t(38,20,'  Installing plugins','muted'),
],[p(36,12,48,10)]);

add('09-success','Install · Result — éxito','L08',[
  ...shell('Result'),
  t(26,14,'Takt was installed for OpenCode.','success'),
  t(26,16,'Files: 12 changed, 4 unchanged'),
  t(26,18,'Changes take effect the next time you start OpenCode.'),
  button(37,22,'Assign models',true), button(56,22,'Back to menu'), button(74,22,'Quit'),
],[p(24,12,71,9)]);

add('10-result-cancelled','Install · Result — cancelado','L09',[
  ...shell('Result'),
  t(21,15,'Installation cancelled before all steps finished.','warning'),
  t(21,17,'Review again checks the actual files first, then applies what is still missing.'),
  button(38,21,'Review again',true), button(56,21,'Back to menu'), button(74,21,'Quit'),
],[p(19,13,82,7)]);

add('11-conflicts','Install · Existing files','L07',[
  ...shell('Conflicts'),
  t(33,14,'~/.config/opencode/takt/agents/takt-dev/OPERATIONS.md','heading'),
  t(33,15,'Source: edited after installation','muted'),
  t(33,16,'Affects: takt-dev sub-agent persona and instructions','muted'),
  t(33,17,'local edits may drift from the supported behavior','warning'),
  bar(33,18),
  t(35,18,'> Keep my version','focus'),
  t(37,19,'Restore Takt version'),
],[p(31,12,58,10)]);

add('12-result-error','Install · Result — error','L09',[
  ...shell('Result'),
  t(29,15,'Failed: permission denied writing ~/.config/opencode/takt/agents/takt-dev/OPERATIONS.md','danger'),
  t(29,17,'Review the error, then retry the installation.'),
  button(40,21,'Retry',true), button(52,21,'Back to menu'), button(70,21,'Quit'),
],[p(27,13,65,7)]);

add('13-targets-invalid','Install · Harnesses — sin selección','L03',[
  ...shell('Harnesses'),
  bar(37,15),
  t(39,15,'> [ ]','muted'), t(45,15,'OpenCode'),
  t(37,19,' * Select at least one harness to continue.','danger'),
],[p(35,13,50,9)]);

add('14-review-blocked','Install · Review — bloqueado','L09',[
  ...shell('Review'),
  t(21,15,'Cannot prepare the review: existing takt.yaml is unreadable (permission denied)','danger'),
  t(21,17,'Nothing was changed. Personalize your choices, or go back and try again.'),
  button(52,21,'Personalize',true),
],[p(19,13,82,7)]);
