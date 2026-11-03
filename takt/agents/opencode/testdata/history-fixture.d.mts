// Types for the scripted owner used by transport fixtures.
export type FixtureExecution = {
 dispatch:string;root:string;unit:string;attempt:string;agent:string;caller:string;
 child?:string;inputs:number[];delivery?:number[];admitted_at:number;
 background?:boolean;settled?:boolean;cycle?:string;role?:string;prompted?:string;launched?:boolean;owner?:string;
}
export type FixtureActivity = {
 activity_id:string;session_id:string;node_kind:string;state:string;flight?:string;outcome?:string;owner?:string;
}
export type FixtureReply={out?:string;err?:string;code?:number}
export function historyFixture(initial?:{
 inputs?:Record<string,number[]>;
 executions?:Record<string,Partial<FixtureExecution>>;
 holders?:Record<string,string>;
 activities?:Record<string,Partial<FixtureActivity>>;
 leases?:string[];
},stageAgents?:string[]):{
 inputs:Map<string,number[]>;executions:Record<string,FixtureExecution>;activities:Record<string,FixtureActivity>;holders:Record<string,string>;
 answer(kind:string,command:string,request:Record<string,unknown>,claims?:unknown[]):FixtureReply|undefined;
 activeChild(child:unknown,agent?:string):FixtureExecution|undefined;
 reply(value:unknown):FixtureReply;denied(err:string):FixtureReply;
}
